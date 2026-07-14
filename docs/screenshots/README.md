# Public screenshot provenance

These screenshots are deterministic 960x720 Catastrophe/Leaf previews generated
from repository fixtures. They contain synthetic public-safe titles, paths, and
statuses: no real API key, signed URL, account name, username, host path,
private library entry, or device log is used. The Settings fixture's masked
suffix is synthetic.

Regenerate the complete set on macOS with:

```sh
make public-screenshots
```

The command rebuilds the app, runs the maintained main/input visual matrices,
and copies only the eight locked public states below.

| Screenshot | Fixture source | SHA-256 |
| --- | --- | --- |
| `main-list.png` | Ready main list, dark theme, hints shown | `f397b9ac98b8570051fc431852a180458602b1eb3f044dd9578d9e97f3729454` |
| `filter-search.png` | Filter with staged search/platform/sort | `09828b490d6c9709ac0768b8883648f96efb966724dea215f7081376e4aab829` |
| `detail-gallery.png` | Detail with generated cover/gallery state | `859e2faab406b4d5a3dfd2d070f162c563979d2402ae9c3276e484440c6e7802` |
| `content-warning.png` | Tag-based content-warning gate | `602d67cbe845902927bcfb77a25197756e48d7ec75227de6a0274cdafa5da3c2` |
| `download-progress.png` | Active Catastrophe progress view | `eb569f943a779738861d005f453d094240aa2421c240f8af31203eb3401fd44b` |
| `settings.png` | Leaf settings list | `40c39acf9fa4a2327365f1756061c7fe3846afed57d00a4fe237588aace84b2c` |
| `dual-sd-destination.png` | Source picker with primary and secondary cards | `350be9e0d313281b15b46cf2f0328616cefb9c74fb6dcaa2d20ed313bc16d87d` |
| `downloaded-manage.png` | Downloaded-file management list | `16c882fdf8be2c9b909c1e99d9ab8ec00e61b9a6eebd515ef1c41b59e2ffc898` |

The PNGs are documentation artifacts, not screenshot test baselines. The
ignored `build/cat-main-list` and `build/cat-input` matrices remain the full
automated visual evidence.
