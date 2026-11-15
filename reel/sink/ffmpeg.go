// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"getsteep.dev/steep/reel/sink/internal/ffmpeg"
)

// ErrFFmpegNotFound is wrapped by the error the [FFmpeg] driver reports, through
// [CreateVideo], [NewVideo], and [CheckVideo], when the ffmpeg binary is not
// found. Its message is meant for whoever runs the program, so it says how to
// install ffmpeg; to use a binary that isn't on PATH, pass its path to
// [FFmpeg].
var ErrFFmpegNotFound = errors.New("sink: ffmpeg not found")

// MissingEncoderError reports an ffmpeg build without any encoder a format
// can use.
type MissingEncoderError struct {
	// Format is the requested video format.
	Format Format
	// Encoders are the ffmpeg encoders the format can use, preferred first.
	Encoders []string
	// FFmpeg is the path of the ffmpeg binary that was probed.
	FFmpeg string
}

func (e *MissingEncoderError) Error() string {
	return fmt.Sprintf(
		"sink: %s needs ffmpeg's %s encoder, but %s was built without it; install an ffmpeg build that includes it",
		e.Format, strings.Join(e.Encoders, " or "), e.FFmpeg,
	)
}

// FFmpeg returns the [Driver] that encodes every video format by running the
// ffmpeg binary at path, or a name looked up on PATH, which "" makes
// "ffmpeg". MP4 is H.264 (libx264), WebM is VP9 (libvpx-vp9), and WebP is
// libwebp_anim or libwebp. Probes of a binary are cached for the life of the
// process.
func FFmpeg(path string) Driver {
	if path == "" {
		path = "ffmpeg"
	}
	return ffmpegDriver{name: path}
}

type ffmpegDriver struct {
	name string
}

// find resolves and probes the binary, and picks the first encoder p can
// use.
func (d ffmpegDriver) find(format Format) (*ffmpeg.Info, videoProfile, string, error) {
	p, ok := videoProfiles[format]
	if !ok {
		return nil, p, "", fmt.Errorf("sink: ffmpeg driver: %s: %w", format, errors.ErrUnsupported)
	}
	info, err := findFFmpeg(d.name, format)
	if err != nil {
		return nil, p, "", err
	}
	for _, enc := range p.encoders {
		if info.HasEncoder(enc) {
			return info, p, enc, nil
		}
	}
	return nil, p, "", &MissingEncoderError{Format: format, Encoders: p.encoders, FFmpeg: info.Path}
}

func (d ffmpegDriver) Check(format Format, _ VideoConfig) error {
	_, _, _, err := d.find(format)
	return err
}

func (d ffmpegDriver) Start(out *os.File, format Format, size image.Point, cfg VideoConfig) (Job, error) {
	info, p, encoder, err := d.find(format)
	if err != nil {
		return nil, err
	}
	// ffmpeg opens the file by name, so it is the only writer.
	if cerr := out.Close(); cerr != nil {
		return nil, cerr
	}
	j := &ffmpegJob{format: format, path: out.Name(), size: size, cfg: cfg}
	j.job, err = ffmpeg.Start(info.Path, ffmpegArgs(info, p, encoder, cfg, out.Name()))
	if err != nil {
		return nil, err
	}
	j.mkv = ffmpeg.NewMKV(j.job.Stdin())
	if err = j.mkv.WriteHeader(size.X, size.Y); err != nil {
		return nil, j.job.End(err)
	}
	return j, nil
}

// findFFmpeg resolves and probes the ffmpeg binary.
func findFFmpeg(name string, format Format) (*ffmpeg.Info, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s export needs ffmpeg; install it (https://ffmpeg.org/download.html or your package manager) and put it on PATH (%w)",
			ErrFFmpegNotFound, format, err,
		)
	}
	return ffmpeg.Probe(path)
}

// videoProfile is how ffmpeg encodes one [Format].
type videoProfile struct {
	// muxer is ffmpeg's output format name.
	muxer string
	// encoders are preferred first.
	encoders []string
	// args returns the encoder arguments after "-c:v <encoder>".
	args func(cfg VideoConfig) []string
}

var videoProfiles = map[Format]videoProfile{ //nolint:exhaustive // GIF and PNG are not video.
	FormatMP4: {
		muxer:    "mp4",
		encoders: []string{"libx264"},
		args: func(cfg VideoConfig) []string {
			crf := 18
			if cfg.Quality >= 0 {
				crf = qualityToCRF(cfg.Quality, 51)
			}
			return []string{
				"-crf", strconv.Itoa(crf),
				"-movflags", "+faststart",
				// MP4 otherwise picks a track timescale from a guessed frame
				// rate, which rounds the millisecond delays.
				"-video_track_timescale", "1000",
			}
		},
	},
	FormatWebM: {
		muxer:    "webm",
		encoders: []string{"libvpx-vp9"},
		args: func(cfg VideoConfig) []string {
			crf := 30
			if cfg.Quality >= 0 {
				crf = qualityToCRF(cfg.Quality, 63)
			}
			args := []string{"-crf", strconv.Itoa(crf), "-b:v", "0", "-row-mt", "1"}
			if cfg.Quality == MaxQuality {
				args = append(args, "-lossless", "1")
			}
			return args
		},
	},
	FormatWebP: {
		muxer: "webp",
		// libwebp_anim stores only what changed between frames, about a
		// seventh the size of libwebp's full frames for a terminal.
		encoders: []string{"libwebp_anim", "libwebp"},
		// The loop count is not passed to ffmpeg: its webp muxer ignores it
		// for libwebp_anim, so [fixWebP] writes it instead.
		args: func(cfg VideoConfig) []string {
			if cfg.Quality >= 0 && cfg.Quality < MaxQuality {
				return []string{"-lossless", "0", "-quality", strconv.Itoa(cfg.Quality)}
			}
			return []string{"-lossless", "1"}
		},
	},
}

// qualityToCRF maps [WithQuality] onto a CRF scale where 0 is best and
// worst is the encoder's largest value.
func qualityToCRF(q, worst int) int {
	return int(math.Round(float64(MaxQuality-q) * float64(worst) / MaxQuality))
}

// ffmpegArgs returns the arguments of an encode from Matroska on stdin to
// path.
func ffmpegArgs(info *ffmpeg.Info, p videoProfile, encoder string, cfg VideoConfig, path string) []string {
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-f", "matroska", "-i", "pipe:0",
		"-map", "0:v:0", "-an", "-sn", "-dn",
	}
	args = append(args, info.PassthroughArgs()...)
	// Keep the stream's millisecond time base rather than a guessed frame rate.
	args = append(args, "-enc_time_base", "-1")
	if p.muxer != "webp" {
		pixFmt := "yuv420p"
		filter := fmt.Sprintf(
			"pad=ceil(iw/2)*2:ceil(ih/2)*2:color=0x%02x%02x%02x,",
			cfg.Background.R, cfg.Background.G, cfg.Background.B,
		)
		if cfg.FullChroma {
			pixFmt, filter = "yuv444p", ""
		}
		// BT.709 is what players assume for untagged and HD video, so convert
		// with it and say so; swscale's default (BT.601) shifts colors.
		args = append(args,
			"-vf", filter+"scale=out_color_matrix=bt709:out_range=tv,format="+pixFmt,
			"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
		)
	}
	args = append(args, "-c:v", encoder)
	args = append(args, p.args(cfg)...)
	return append(args, "-f", p.muxer, "-y", path)
}

// ffmpegJob streams frames to ffmpeg as uncompressed Matroska.
type ffmpegJob struct {
	format Format
	path   string
	size   image.Point
	cfg    VideoConfig
	job    *ffmpeg.Job
	mkv    *ffmpeg.MKV
}

func (j *ffmpegJob) WriteFrame(img *image.NRGBA, delay time.Duration) error {
	w, h := j.size.X, j.size.Y
	err := j.mkv.WriteFrame(w*h*4, delay, func(out io.Writer) error {
		_, err := out.Write(img.Pix[:w*h*4])
		return err
	})
	if err != nil {
		return j.job.End(err)
	}
	return nil
}

func (j *ffmpegJob) Close() error {
	if err := j.job.End(nil); err != nil {
		return err
	}
	if j.format == FormatWebP {
		if err := fixWebP(j.path, j.cfg.Plays, j.mkv.Duration()); err != nil {
			return fmt.Errorf("sink: finishing %s: %w", j.format, err)
		}
	}
	return nil
}

func (j *ffmpegJob) Abort() { j.job.Kill() }
