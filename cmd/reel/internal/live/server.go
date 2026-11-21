// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"getsteep.dev/steep/cmd/reel/internal/spec"
	"getsteep.dev/steep/reel/effects"

	"getsteep.dev/steep/cmd/reel/internal/play"
	"getsteep.dev/steep/reel"
	"getsteep.dev/steep/reel/region"
	"getsteep.dev/steep/reel/vt"
)

// Config is how [Serve] starts a session.
type Config struct {
	Dir, Name  string
	Cols, Rows int
	Command    []string
	// Env is added to the host's environment; WorkDir is the program's
	// working directory.
	Env     []string
	WorkDir string
	// Linger is how long the server stays up once the program exits, so
	// its last screen and recording can still be read.
	Linger time.Duration
	// Ready is called once the session is listening, or with the error
	// that stopped it from starting.
	Ready func(error)
}

// server is a running session.
type server struct {
	s    *reel.Session
	info Info
	// input is held by the request sending the program input, so that the
	// keys of two requests do not interleave.
	input sync.Mutex
	// mu guards info's exit status.
	mu       sync.Mutex
	exited   chan struct{}
	stop     context.CancelFunc
	stopOnce sync.Once
}

// caption returns the annotation of the caption or callout req asks for.
func caption(req Request) (reel.Annotation, error) {
	if !req.Callout {
		opts, err := spec.Caption(req.Spec)
		if err != nil {
			return reel.Annotation{}, err
		}
		return effects.NewCaption(req.Text, opts...)
	}
	region, opts, err := spec.Callout(req.Spec)
	if err != nil {
		return reel.Annotation{}, err
	}
	return effects.NewCallout(region, req.Text, opts...)
}

// Serve starts the program cfg names on a session, and serves requests
// for it until a stop request, or cfg.Linger after the program exits, or
// ctx is done.
func Serve(ctx context.Context, cfg Config) (err error) {
	ready := func(readyErr error) {
		if cfg.Ready != nil {
			cfg.Ready(readyErr)
			cfg.Ready = nil
		}
	}
	defer func() { ready(err) }()
	if !ValidName(cfg.Name) {
		return errInvalidName(cfg.Name)
	}
	if len(cfg.Command) == 0 {
		return errors.New("no command")
	}
	// The name's lock, held for the server's life, makes the server the
	// only one that removes or binds its socket.
	lk, err := lock(filepath.Join(cfg.Dir, cfg.Name+".lock"))
	if errors.Is(err, errLocked) {
		return fmt.Errorf("session %q is already running", cfg.Name)
	}
	if err != nil {
		return err
	}
	defer lk.Close()
	sock := Socket(cfg.Dir, cfg.Name)
	if sockErr := checkSocket(sock); sockErr != nil {
		return sockErr
	}
	_ = os.Remove(sock) // A stale socket of a server that is gone.

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", sock)
	if err != nil {
		return err
	}
	defer os.Remove(sock)
	defer ln.Close()
	s, err := reel.Open(ctx, cfg.Cols, cfg.Rows, reel.WithTitle(cfg.Name))
	if err != nil {
		return err
	}
	defer s.Close()
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...) //nolint:gosec // Running it is the point.
	if cfg.Env != nil {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	cmd.Dir = cfg.WorkDir
	p, err := reel.Exec(ctx, s, cmd)
	if err != nil {
		return err
	}

	srv := &server{
		s:      s,
		exited: make(chan struct{}),
		stop:   stop,
		info: Info{
			Name: cfg.Name, PID: cmd.Process.Pid, Command: cfg.Command,
			Cols: cfg.Cols, Rows: cfg.Rows, Started: time.Now(),
		},
	}
	go func() {
		status, _ := p.Wait()
		srv.mu.Lock()
		srv.info.Exited, srv.info.Status = true, status
		srv.mu.Unlock()
		close(srv.exited)
		select {
		case <-time.After(cfg.Linger):
			stop()
		case <-ctx.Done():
		}
	}()
	context.AfterFunc(ctx, func() { _ = ln.Close() })
	ready(nil)

	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return acceptErr
		}
		wg.Go(func() { srv.serve(ctx, conn) })
	}
}

// serve answers the one request of conn.
func (srv *server) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()
	var req Request
	var resp *Response
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		resp = &Response{Error: fmt.Sprintf("invalid request: %v", err), Code: CodeInvalid}
	} else {
		resp = srv.handle(ctx, req)
	}
	_ = json.NewEncoder(conn).Encode(resp)
	// Windows resets a connection closed with input unread, which can
	// discard the response before the client reads it; ending the output,
	// then waiting a moment for the client to close, closes it cleanly.
	if uc, ok := conn.(*net.UnixConn); ok && uc.CloseWrite() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}
	if req.Op == OpStop && resp.Error == "" {
		srv.stopOnce.Do(srv.stop)
	}
}

// inputOps are the ops that send the program input, and so need it not to
// have exited, and run one at a time.
var inputOps = map[string]bool{
	OpType: true, OpKey: true, OpPaste: true, OpClick: true, OpHover: true, OpDrag: true, OpWheel: true, OpDo: true,
}

// buttons are the mouse buttons a click or drag names, and wheelButtons the
// directions a wheel turn does.
var (
	buttons      = map[string]vt.MouseButton{"": vt.MouseLeft, "left": vt.MouseLeft, "middle": vt.MouseMiddle, "right": vt.MouseRight}
	wheelButtons = map[string]vt.MouseButton{
		"up": vt.MouseWheelUp, "down": vt.MouseWheelDown, "left": vt.MouseWheelLeft, "right": vt.MouseWheelRight,
	}
)

// mouse answers a click, drag, hover, or wheel op.
func mouse(ctx context.Context, s *reel.Session, req Request) *Response {
	at, to := image.Pt(req.X, req.Y), image.Pt(req.ToX, req.ToY)
	cells := []image.Point{at}
	if req.Op == OpDrag {
		cells = append(cells, to)
	}
	button, ok := buttons[req.Button]
	if req.Op == OpWheel {
		button, ok = wheelButtons[req.Button]
	}
	switch {
	case req.Op == OpWheel && !ok:
		return &Response{Error: fmt.Sprintf("unknown wheel direction %q, want up, down, left, or right", req.Button), Code: CodeInvalid}
	case req.Op != OpHover && !ok:
		return &Response{Error: fmt.Sprintf("unknown button %q, want left, middle, or right", req.Button), Code: CodeInvalid}
	}
	if err := play.OnScreen(s, req.Op, cells...); err != nil {
		return &Response{Error: err.Error(), Code: CodeInvalid}
	}
	var err error
	switch req.Op {
	case OpClick:
		err = s.Click(button, at.X, at.Y)
	case OpDrag:
		err = play.Drag(ctx, s, button, at, to, req.Delay)
	case OpWheel:
		err = play.Wheel(ctx, s, button, at.X, at.Y, max(req.Repeat, 1), req.Delay)
	default:
		err = s.Mouse(vt.MouseMotion, vt.MouseUnknown, at.X, at.Y)
	}
	if err != nil {
		return failed(err)
	}
	return &Response{}
}

func (srv *server) handle(ctx context.Context, req Request) *Response {
	s := srv.s
	if inputOps[req.Op] {
		srv.input.Lock()
		defer srv.input.Unlock()
		select {
		case <-srv.exited:
			return &Response{Error: "the program has exited", Code: CodeExited}
		default:
		}
	}
	var err error
	resp := &Response{}
	switch req.Op {
	case OpInfo:
		srv.mu.Lock()
		info := srv.info
		srv.mu.Unlock()
		info.Cols, info.Rows = s.Terminal().Size()
		resp.Info = &info
	case OpType:
		err = play.Type(ctx, s, req.Text, req.Delay)
	case OpKey:
		for _, k := range req.Keys {
			if err = play.Press(ctx, s, k, max(req.Repeat, 1), req.Delay); err != nil {
				break
			}
		}
	case OpPaste:
		err = s.Paste(req.Text)
	case OpClick, OpDrag, OpHover, OpWheel:
		return mouse(ctx, s, req)
	case OpScroll:
		if req.Repeat < 0 {
			err = s.ScrollDown(-req.Repeat)
		} else {
			err = s.ScrollUp(req.Repeat)
		}
	case OpResize:
		err = s.Resize(req.Cols, req.Rows)
	case OpMark:
		err = s.Mark(req.Label)
	case OpCaption:
		a, capErr := caption(req)
		if capErr != nil {
			return &Response{Error: capErr.Error(), Code: CodeInvalid}
		}
		err = s.Annotate(a)
	case OpEndCaption:
		a := effects.EndCaption()
		if req.Callout {
			a = effects.EndCallout()
		}
		err = s.Annotate(a)
	case OpWait:
		return srv.wait(ctx, req.Wait)
	case OpScreen:
		text, screenErr := s.ScreenText()
		if screenErr != nil {
			return failed(screenErr)
		}
		resp.Text = play.TrimScreen(text.Text)
		if req.JSON {
			resp.Screen = s.Screen()
		}
	case OpCast:
		var buf bytes.Buffer
		_, err = s.Timeline().WriteCast(&buf, reel.CastV2)
		resp.Cast = buf.Bytes()
	case OpDo:
		var tp *play.Tape
		if tp, err = play.Parse(strings.NewReader(req.Text)); err == nil {
			err = tp.Play(ctx, s)
		}
	case OpStop:
	default:
		return &Response{Error: fmt.Sprintf("unknown op %q", req.Op), Code: CodeInvalid}
	}
	if err != nil {
		return failed(err)
	}
	return resp
}

func failed(err error) *Response {
	return &Response{Error: err.Error(), Code: CodeFailed}
}

// settleOptions returns the options of w's idle wait: its Ignore and Only
// regions.
func settleOptions(w *Wait) ([]reel.SettleOption, error) {
	var opts []reel.SettleOption
	for _, set := range []struct {
		name    string
		specs   []string
		options func(...region.Region) reel.SettleOption
	}{{"ignore", w.Ignore, reel.SettleIgnore}, {"only", w.Only, reel.SettleOnly}} {
		for _, s := range set.specs {
			r, err := spec.Region(s)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", set.name, err)
			}
			opts = append(opts, set.options(r))
		}
	}
	return opts, nil
}

// wait answers an [OpWait].
func (srv *server) wait(ctx context.Context, w *Wait) *Response {
	if w == nil {
		return &Response{Error: "wait: nothing to wait for", Code: CodeInvalid}
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if w.Idle <= 0 && (len(w.Ignore) > 0 || len(w.Only) > 0) {
		return &Response{Error: "wait: ignore and only need idle", Code: CodeInvalid}
	}
	var cond reel.Condition
	switch {
	case w.Text != "":
		cond = reel.Contains(w.Text)
	case w.Gone != "":
		cond = reel.NotContains(w.Gone)
	case w.Regexp != "":
		re, reErr := regexp.Compile(w.Regexp)
		if reErr != nil {
			return &Response{Error: fmt.Sprintf("wait: %v", reErr), Code: CodeInvalid}
		}
		cond = reel.Matches(re)
	case w.Idle > 0:
		opts, optsErr := settleOptions(w)
		if optsErr != nil {
			return &Response{Error: fmt.Sprintf("wait: %v", optsErr), Code: CodeInvalid}
		}
		text, settleErr := srv.s.WaitSettle(ctx, w.Idle, opts...)
		return waited(text, settleErr, timeout)
	default:
		return &Response{Error: "wait: give text, gone, regexp, or idle", Code: CodeInvalid}
	}
	if w.CursorLine {
		cond = reel.OnCursorLine(cond)
	}
	res, err := srv.s.Wait(ctx, cond)
	return waited(res.Text, err, timeout)
}

// waited is the response to a wait that ended with text on the screen and
// err.
func waited(text string, err error, timeout time.Duration) *Response {
	var werr *reel.WaitError
	switch {
	case err == nil:
		return &Response{Text: play.TrimScreen(text)}
	case errors.As(err, &werr) && errors.Is(werr.Err, context.DeadlineExceeded):
		return &Response{Error: fmt.Sprintf("wait for %s timed out after %v", werr.Wait, timeout), Code: CodeTimeout, Text: play.TrimScreen(werr.Last)}
	}
	return failed(err)
}
