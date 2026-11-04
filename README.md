<!-- template:define:options
{
  "nodescription": true
}
-->
![banner](https://github.com/user-attachments/assets/28ee5cf7-bd21-4c88-82bf-a17eb01e2394)

<!-- template:begin:header -->
<!-- do not edit anything in this "template" block, its auto-generated -->

<p align="center">
  <a href="https://github.com/lrstanley/steep/tags">
    <img title="Latest Semver Tag" src="https://img.shields.io/github/v/tag/lrstanley/steep?style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/steep/commits/master">
    <img title="Last commit" src="https://img.shields.io/github/last-commit/lrstanley/steep?style=flat-square">
  </a>




  <a href="https://github.com/lrstanley/steep/actions?query=workflow%3Atest+event%3Apush">
    <img title="GitHub Workflow Status (test @ master)" src="https://img.shields.io/github/actions/workflow/status/lrstanley/steep/test.yml?branch=master&label=test&style=flat-square">
  </a>


  <a href="https://codecov.io/gh/lrstanley/steep">
    <img title="Code Coverage" src="https://img.shields.io/codecov/c/github/lrstanley/steep/master?style=flat-square">
  </a>

  <a href="https://pkg.go.dev/github.com/lrstanley/steep">
    <img title="Go Documentation" src="https://pkg.go.dev/badge/github.com/lrstanley/steep?style=flat-square">
  </a>
  <a href="https://goreportcard.com/report/github.com/lrstanley/steep">
    <img title="Go Report Card" src="https://goreportcard.com/badge/github.com/lrstanley/steep?style=flat-square">
  </a>
</p>
<p align="center">
  <a href="https://github.com/lrstanley/steep/issues?q=is:open+is:issue+label:bug">
    <img title="Bug reports" src="https://img.shields.io/github/issues/lrstanley/steep/bug?label=issues&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/steep/issues?q=is:open+is:issue+label:enhancement">
    <img title="Feature requests" src="https://img.shields.io/github/issues/lrstanley/steep/enhancement?label=feature%20requests&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/steep/pulls">
    <img title="Open Pull Requests" src="https://img.shields.io/github/issues-pr/lrstanley/steep?label=prs&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/steep/discussions/new?category=q-a">
    <img title="Ask a Question" src="https://img.shields.io/badge/support-ask_a_question!-blue?style=flat-square">
  </a>
  <a href="https://liam.sh/chat"><img src="https://img.shields.io/badge/discord-bytecord-blue.svg?style=flat-square" title="Discord Chat"></a>
</p>
<!-- template:end:header -->

<!-- template:begin:toc -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :link: Table of Contents

  - [Features](#sparkles-features)
  - [Usage](#gear-usage)
    - [Related libraries](#related-libraries)
  - [Support &amp; Assistance](#raising_hand_man-support--assistance)
  - [Contributing](#handshake-contributing)
  - [License](#balance_scale-license)
<!-- template:end:toc -->

## :sparkles: Features

**steep** is a test harness for [Bubble Tea](https://github.com/charmbracelet/bubbletea) programs and components. It runs the program in an emulated terminal (a [reel](reel) session), drives it with keys, text, mouse events, and resizes, and asserts on what it draws.

> [!WARNING]
> steep is still pre-1.0. Its API may change drastically until the v1.0.0 release, so expect breaking changes
> between versions, and pin the version you use.

---

## :gear: Usage

```go
func TestModel(t *testing.T) {
	steep.NewHarness(t, newModel()).
		Wait("Tokyo").
		Key("down").
		AssertSnapshot()
}
```

### Recording

Everything the program draws is kept in the session's timeline, and nothing is
written unless an export policy says so. `WithRecording` takes a `reel.Profile`:
what to write, how to draw it, and the effects to apply.

```go
h := steep.NewHarness(t, newModel(),
	steep.WithRecording(reel.Profile{
		Outputs: reel.MustFiles("{name}_{status}.gif", "{name}_{status}.cast"),
		Effects: []reel.Effect{
			steep.Logo(2 * time.Second),
			effects.Keys(time.Second),
			effects.Pause(),
			steep.Outro(2 * time.Second),
		},
	}),
	steep.WithExportOnFailure(), // or steep.WithExportAlways()
)
```

The export runs once, when the harness closes. `effects.Pause()` pauses for a
second on the last frame, with a pause badge. For a failed test, `steep.Outro`
stretches that pause to at least 3s, then shows a FAIL card listing the
failures. Files go to `t.ArtifactDir()` (kept with `go test -artifacts`),
or to `WithArtifacts(dir)`. The `.cast` file is the recording itself:
exporting it again with `reel.Export`, the same profile, and the same
`steep.Result` writes the same GIF.

The `.cast` file also holds what the test did and found, as annotations that
players ignore: the harness's configuration, each input, wait, assertion,
and snapshot comparison with its outcome, each failure, the result, and the
program's terminal queries with the replies. reel's
[`annotation.schema.json`](reel/annotation.schema.json) describes them.
`steep.WithRecordMessages(true)` adds every message the model receives,
except `tea.EnvMsg`, which may hold secrets, and `steep.WithSource(true)`
the file, line, and function of the test's line behind each annotation and
mark, with paths relative to the module's root.

Any test can be recorded without changing it:

```sh
STEEP_EXPORT_MODE=on STEEP_EXPORT='{name}.gif?max-bytes=5MB,{name}.cast' go test -artifacts ./...
```

| Variable | Sets |
| --- | --- |
| `STEEP_EXPORT_MODE` | `on`, `failure`, or `off` |
| `STEEP_EXPORT` | comma-separated outputs, each a path with optional query parameters (see `reel.ParseFile`), such as `{name}.gif?loop=once`; a path with `{mark}` and no `marks=` or `match=` is a still at each hook mark, `?match=input:*` narrows it to matching marks, and `.snap` writes the screen's text. An entry that does not parse fails the test. |
| `STEEP_EXPORT_DIR` | the artifacts directory |
| `STEEP_EXPORT_FPS`, `STEEP_EXPORT_SPEED` | the profile's timing |
| `STEEP_RENDERER_SCALE`, `STEEP_RENDERER_FONT_FAMILY` | renderer options |
| `STEEP_RECORD_MESSAGES` | `on` to record every message the model receives |
| `STEEP_SOURCE` | `on` to record the test's file, line, and function for each annotation and mark (`steep.WithSource`) |

See [`_examples/steep-showcase`](_examples/steep-showcase/main_test.go) for a
complete example.

### Related libraries

- [reel](reel): records terminals and renders recordings, from Go or with the
  [`reel` CLI](cmd/reel). steep records through it.
- [bubbletint](https://github.com/lrstanley/bubbletint): hundreds of terminal color themes for Bubble Tea and Lip
  Gloss.
- [bloom](https://bloomfx.dev): a layout, compositing, and animation engine for Bubble Tea programs. Its end-to-end
  tests and example recordings run on steep.

<!-- template:begin:goget -->
<!-- do not edit anything in this "template" block, its auto-generated -->
```console
go get -u github.com/lrstanley/steep@latest
```
<!-- template:end:goget -->

---

<!-- template:begin:support -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :raising_hand_man: Support & Assistance

* :heart: Please review the [Code of Conduct](.github/CODE_OF_CONDUCT.md) for
     guidelines on ensuring everyone has the best experience interacting with
     the community.
* :raising_hand_man: Take a look at the [support](.github/SUPPORT.md) document on
     guidelines for tips on how to ask the right questions.
* :lady_beetle: For all features/bugs/issues/questions/etc, [head over here](https://github.com/lrstanley/steep/issues/new/choose).
<!-- template:end:support -->

<!-- template:begin:contributing -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :handshake: Contributing

* :heart: Please review the [Code of Conduct](.github/CODE_OF_CONDUCT.md) for guidelines
     on ensuring everyone has the best experience interacting with the
    community.
* :clipboard: Please review the [contributing](.github/CONTRIBUTING.md) doc for submitting
     issues/a guide on submitting pull requests and helping out.
* :old_key: For anything security related, please review this repositories [security policy](https://github.com/lrstanley/steep/security/policy).
<!-- template:end:contributing -->

<!-- template:begin:license -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :balance_scale: License

```
MIT License

Copyright (c) 2026 Liam Stanley <liam@liam.sh>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

_Also located [here](LICENSE)_
<!-- template:end:license -->
