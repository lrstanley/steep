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

  <a href="https://pkg.go.dev/getsteep.dev/steep">
    <img title="Go Documentation" src="https://pkg.go.dev/badge/getsteep.dev/steep?style=flat-square">
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
    - [The](#the-reel-cli)
    - [Related libraries](#related-libraries)
  - [Development](#hammer_and_wrench-development)
  - [Support &amp; Assistance](#raising_hand_man-support--assistance)
  - [Contributing](#handshake-contributing)
  - [License](#balance_scale-license)
<!-- template:end:toc -->

## :sparkles: Features

**steep** is a test harness for terminal programs in Go. It runs your program in an emulated terminal, drives it with
keys, text, mouse events, and resizes, and checks what it draws, all from `go test`.

> [!WARNING]
> steep is still pre-1.0. Its API may change drastically until the v1.0.0 release, so expect breaking changes
> between versions, and pin the version you use.

- :heavy_check_mark: **Three harnesses, one API**: run a Bubble Tea model, a single component, or any command on a
  pseudo-terminal, with the same inputs, waits, assertions, snapshots, and recordings.
- :heavy_check_mark: **Waits, not sleeps**: wait on what's on the screen, a regular expression, or a message your model
  received, so tests don't depend on how fast the machine is.
- :heavy_check_mark: **Real input**: keys, paste, clicks, and drags go through the terminal as bytes, encoded for whatever
  keyboard and mouse modes your program asked for. `Locate` clicks what's on the screen rather than a coordinate.
- :heavy_check_mark: **Snapshots**: golden files of the screen's text, or of every cell and its style.
- :heavy_check_mark: **Terminal state**: check the title, cursor, clipboard, progress, notifications, and more.
- :heavy_check_mark: **Recordings of failures**: export a GIF, WebP, video, or `.cast` of any test, with no code changes,
  through `STEEP_EXPORT_MODE`, and step through it event by event in the
  [inspector](https://inspector.getsteep.dev).
- :heavy_check_mark: **No cgo**: the terminal emulator is compiled in, so there's nothing else to install.

---

## :gear: Usage

Take a look at the [official documentation](https://getsteep.dev/) for tutorials, guides, and reference, starting with
[your first test](https://getsteep.dev/steep/first-test). The documentation is also available for LLMs and agents as
[llms.txt](https://getsteep.dev/llms.txt) and [llms-full.txt](https://getsteep.dev/llms-full.txt), and every page has a
markdown version.

```go
func TestMenu(t *testing.T) {
	steep.NewHarness(t, newModel()).
		Wait("Sencha").
		KeyDown().
		Assert("Matcha").
		Key("enter").
		Wait("Matcha is ready").
		AssertSnapshot()
}
```

Runnable examples are in [`_examples`](_examples).

<!-- template:begin:goget -->
<!-- do not edit anything in this "template" block, its auto-generated -->
```console
go get -u getsteep.dev/steep@latest
```
<!-- template:end:goget -->

### The `reel` CLI

Install `reel` on macOS or Linux with [Homebrew](https://brew.sh):

```console
brew install lrstanley/tap/reel
```

Or download it for Linux, macOS, or Windows (amd64 or arm64) from [the releases page](https://github.com/lrstanley/steep/releases),
with checksums in `steep_checksums.txt`, or build it with Go:

```console
go install getsteep.dev/steep/cmd/reel@latest
```

See [the CLI docs](https://getsteep.dev/cli#installation) for the steps.

### Related libraries

- [reel](https://getsteep.dev/reel): records terminals and renders recordings, from Go. steep records through it.
- [`reel` CLI](https://getsteep.dev/cli): the same without Go. It runs VHS tapes, tests with them, drives programs for
  scripts and agents, and renders `.cast` files.
- [bubbletint](https://github.com/lrstanley/bubbletint): hundreds of terminal color themes for Bubble Tea and Lip
  Gloss.
- [bloom](https://bloomfx.dev): a layout, compositing, and animation engine for Bubble Tea programs. Its end-to-end
  tests and example recordings run on steep.

## :hammer_and_wrench: Development

This repository holds several Go modules. To work on them, install [Task](https://taskfile.dev), then run this once
after cloning:

```console
task setup
```

It creates a gitignored `go.work`, so changes to [reel](reel) apply to steep and the CLI straight away, the same as
in CI. Without it, they build against the reel version they pin. Nothing runs this for you.

`task --list` shows the rest, including the [documentation site](docs) (`task docs:dev`) and the
[inspector](inspector) (`task inspector:dev`), which needs Go and [Bun](https://bun.sh).

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
