# Third-party notices

pixeltui is licensed under the MIT License (see [LICENSE](LICENSE)).

It builds on third-party software in two distinct ways, with different
obligations for each.

---

## 1. Go libraries compiled into the binary

These open-source Go modules are statically linked into the `pixeltui` binary.
All are permissive (MIT or BSD-3-Clause) and require only that their copyright
and permission notices be preserved — which this file does. Copyright remains
with each project's respective authors.

### MIT License

- github.com/aymanbagabas/go-osc52/v2
- github.com/charmbracelet/bubbles
- github.com/charmbracelet/bubbletea
- github.com/charmbracelet/colorprofile
- github.com/charmbracelet/harmonica
- github.com/charmbracelet/lipgloss
- github.com/charmbracelet/x/ansi
- github.com/charmbracelet/x/cellbuf
- github.com/charmbracelet/x/term
- github.com/clipperhouse/displaywidth
- github.com/clipperhouse/stringish
- github.com/clipperhouse/uax29/v2
- github.com/lucasb-eyer/go-colorful
- github.com/mattn/go-isatty
- github.com/mattn/go-runewidth
- github.com/muesli/ansi
- github.com/muesli/cancelreader
- github.com/muesli/termenv
- github.com/raitonoberu/ytmusic
- github.com/rivo/uniseg
- github.com/sahilm/fuzzy
- github.com/xo/terminfo
- go.etcd.io/bbolt

### BSD 3-Clause License

- github.com/atotto/clipboard — Copyright (c) 2013 Ato Araki
- golang.org/x/sys — Copyright (c) 2009 The Go Authors

The full text of each license is reproduced at the end of this file.

---

## 2. External tools invoked at runtime

pixeltui calls these command-line programs as **separate processes** (via the
shell / a local IPC socket). They are installed by the user (or by
`pixeltui doctor --fix` / the install scripts) and are **not** bundled with,
linked into, or redistributed as part of pixeltui. Their licenses govern those
tools, not pixeltui, and no copyleft terms extend to this project.

| Tool   | Role | License |
|--------|------|---------|
| **yt-dlp** | Resolves and downloads YouTube audio streams | The Unlicense (public domain) |
| **mpv**    | Playback engine (pause/seek/volume, Now Playing) | GPL-2.0-or-later / LGPL-2.1-or-later |
| **ffmpeg** (`ffprobe`, `ffplay`) | Local-file tags and fallback playback | LGPL-2.1-or-later or GPL-2.0-or-later, depending on the build |

If you redistribute these tools alongside pixeltui (for example, in a bundle or
container), you are responsible for complying with their respective licenses.

---

## License texts

### MIT License

```
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

### BSD 3-Clause License

```
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
   this list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.
3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
