# Third-party notices

This project incorporates or depends on third-party software. The shipped Pak
must retain the applicable license texts and notices. This inventory is updated
as the Leaf port changes its renderer and packaging dependencies.

## Forked source

- `carroarmato0/NextUI-Itchio-Pak` — MIT; see `LICENSE` and `UPSTREAM.md`.

## Bundled assets

- Noto Sans font assets — Apache License 2.0; see
  `assets/Apache-2.0-NotoSans.txt`.
- Noto Emoji font asset — SIL Open Font License 1.1; see
  `assets/OFL-1.1-NotoEmoji.txt`.
- Noto Sans JP font asset — SIL Open Font License 1.1; see
  `assets/OFL-1.1-NotoSansJP.txt`.
- `assets/ca-certificates.crt` — aggregate CA certificate data; certificate
  owners retain their respective rights and terms.

## Go and native dependencies

The authoritative dependency set is `go.mod`/`go.sum` plus the native libraries
selected by the build. The current Go build list is:

| Module family | License |
|---|---|
| `github.com/holoplot/go-evdev` | MIT |
| `github.com/refraction-networking/utls` | BSD 3-Clause |
| `github.com/skip2/go-qrcode` | MIT |
| `golang.org/x/{crypto,image,net,sys,text}` | BSD 3-Clause |
| `github.com/andybalholm/brotli` | MIT |
| `github.com/bodgit/{plumbing,sevenzip,windows}` | BSD 3-Clause |
| `github.com/gaukas/godicttls` | BSD 3-Clause |
| `github.com/hashicorp/{errwrap,go-multierror}` | Mozilla Public License 2.0 |
| `github.com/klauspost/compress` | BSD 3-Clause |
| `github.com/pierrec/lz4/v4` | BSD 3-Clause |
| `github.com/ulikunitz/xz` | BSD 3-Clause |
| `go4.org` | Apache License 2.0 |

Versions are pinned in `go.mod` and `go.sum`; those files are authoritative when
this human-readable grouping becomes stale. Their upstream license files must
be included in release notices by the packaging lane.

Catastrophe is an MIT-licensed production dependency through the app-local CGo
bridge; its license is shipped as `licenses/Catastrophe-LICENSE`. SDL remains a
native runtime dependency of Catastrophe; the retired Go SDL binding is no
longer part of the module graph. Before the first Pak Rat submission, the
release gate must generate and review a complete dependency/license manifest,
including native libraries, image decoders, archive readers, and transitive Go
modules.

No production package may pass the license verification gate while that
manifest contains an unknown or incompatible license.
