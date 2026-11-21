# pglens-pgext test image

`pglens-pgext:18` is `postgres:18.6-alpine` with two extensions the integration
tests need and the stock image lacks:

| Extension | Version | Used by |
|-----------|---------|---------|
| [hypopg](https://github.com/HypoPG/hypopg) | 1.4.5 | index suggestion validation (`index_check` command) |
| [pg_hint_plan](https://github.com/ossc-db/pg_hint_plan) | REL18_1_8_1 | tuning workbook hint variants |

Build it with `make pgext-image` (runs `build.sh`). The script downloads both
source archives into `test/pgext/src/` (ignored by Git) with `curl`, then runs
`docker build --network host` with the proxy variables of the shell. The image
stays local; nothing is pushed.

If `pg_hint_plan` does not compile, the build still succeeds and leaves the
marker `/pg_hint_plan.FAILED` in the image; the hint tests skip on it. Check the
result with:

```sh
docker run --rm pglens-pgext:18 sh -c 'ls /usr/local/lib/postgresql/hypopg.so /usr/local/lib/postgresql/pg_hint_plan.so; ls /pg_hint_plan.FAILED'
```

To change a version, edit the URL in `build.sh` and the table above, then
rebuild. Remove the image with `docker rmi pglens-pgext:18`.
