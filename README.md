# scorer

Bridge scoring and movements for the Bulgarian Bridge Federation: pairs (Mitchell, Howell; MP and
IMP; sessions, qualification, finals) and teams (IMP→VP, BAM; Swiss, Danish, round robin, knockout;
groups that split and exchange teams). Offline at the venue, BridgeMate for result entry, results to
bridge.bg. See [docs/DESIGN.md](docs/DESIGN.md).

```sh
go test ./...
```

Status: the engine (`internal/score`, `movement`, `pairs`, `teams`), USEBIO export (`internal/usebio`)
and storage (`internal/store`, one SQLite file per event) are in place and tested. BridgeMate, the
bridge.bg link and the UI are next.

The USEBIO and store tests also run bridge.bg's own parser on what they write, when `../bridge.bg`
and `php` are there (`BRIDGEBG_DIR` points them at another checkout).
