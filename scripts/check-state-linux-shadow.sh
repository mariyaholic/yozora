#!/usr/bin/env bash
set -euo pipefail
repo=${1:-$(cd "$(dirname "$0")/.." && pwd)}
# Logic-only test copy: Windows integrations below are stubbed, not exercised.
d=$(mktemp -d /tmp/yozora-linux-shadow.XXXXXX)
trap 'rm -rf "$d"' EXIT
mkdir -p "$d/internal"
cp "$repo/go.mod" "$repo/go.sum" "$d/"
for p in art browserwin config discordipc player presence smtc template spotifyapi; do cp -r "$repo/internal/$p" "$d/internal/"; done
find "$d/internal" -name '*.go' -exec sed -i '1{/^\/\/go:build windows$/d;}' {} +
rm -f "$d/internal/browserwin/windows.go" "$d/internal/browserwin/uia_"*.go "$d/internal/browserwin/"*_test.go
cat > "$d/internal/browserwin/os_linux.go" <<'GO'
package browserwin
import "errors"
type Window struct { HWND uintptr; Title string }
func Enumerate() []Window { return nil }
func ReadAddressBar(uintptr) (string,error) { return "",errors.New("stub") }
func ReadTabNames(uintptr,int) ([]string,error) { return nil,errors.New("stub") }
GO
# The player table is portable, so it is tested as shipped; only the WinRT session reader is stubbed.
rm -rf "$d/internal/smtc"; mkdir "$d/internal/smtc"
cp "$repo/internal/smtc/players.go" "$repo/internal/smtc/players_test.go" "$d/internal/smtc/"
cat > "$d/internal/smtc/stub.go" <<'GO'
package smtc
import "time"
type Track struct { AppID,Title,Subtitle,Artist,AlbumTitle,AlbumArtist string; TrackNumber int32; Playing bool; PositionSec,DurationSec float64; LastUpdated time.Time; Thumb func()([]byte,error) }
GO
mkdir "$d/internal/credman"
cat > "$d/internal/credman/stub.go" <<'GO'
package credman
import "errors"
func Get(string)(string,error){return "",errors.New("stub")}
func Set(string,string)error{return nil}
func Delete(string)error{return nil}
GO
rm "$d/internal/discordipc/"*_test.go
python3 - "$d/internal/discordipc/ipc.go" <<'PY'
import sys
p=sys.argv[1]
s=open(p).read().replace('\n\t"github.com/Microsoft/go-winio"\n','\n')
a=s.index('func dialPipe('); b=s.index('\nfunc Dial()',a)
s=s[:a]+'func dialPipe(path string) (Transport, error) { return nil, errors.New("Linux test stub") }\n'+s[b:]
open(p,'w').write(s)
PY
cd "$d"
go test ./internal/presence ./internal/spotifyapi ./internal/player ./internal/config ./internal/art ./internal/template ./internal/smtc -count=10
go test -race ./internal/presence ./internal/spotifyapi  -count=100
