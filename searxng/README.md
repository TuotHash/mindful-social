# SearXNG (local dev search backend)

Web search for AI node drafting. Optional — without it, drafts can still be
grounded by pasting source URLs; only the "Search the web" option needs it.

## Local dev (macOS/Linux)

```sh
./searxng/run.sh                 # starts SearXNG on 127.0.0.1:8888 (Docker)
```

Then run the app with:

```sh
SEARXNG_URL=http://127.0.0.1:8888 AI_ENDPOINT_URL=... AI_MODEL=... go run ./cmd/server
```

`settings.yml` enables the JSON API (disabled in SearXNG by default) and turns
off the bot limiter, which the app's programmatic queries need.

## Production

Don't use this script. Run SearXNG natively with the NixOS `services.searx`
module and set `SEARXNG_URL` to it (see `nix/module.nix`). Enable the JSON
format and set a real `server.secret_key` there.
