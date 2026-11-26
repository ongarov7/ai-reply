# AI-Reply campaign redirect

Use `https://target.ai-reply.kz/` as the campaign URL. The existing Caddy instance serves this project on its own hostname. The website and API continue to use their existing Caddy routes.

| Visitor | HTTPS 302 destination |
| --- | --- |
| iPhone and iPad | [App Store](https://apps.apple.com/kz/app/ai-reply/id6818570150) |
| Android | [Google Play](https://play.google.com/store/apps/details?id=kz.yerek.aireply) |
| Desktop, unknown device, or known link preview crawler | [AI-Reply](https://ai-reply.kz/) |

Routing uses the User-Agent and the platform client hint when available. An iPad sending an identical macOS request goes to the website. Meta's preview and ads crawlers follow the website redirect to its Open Graph metadata. `GET` and `HEAD` use the same selection, with `Cache-Control: private, no-store, max-age=0`.

## Deploy

1. In the Hoster.kz DNS zone for `ai-reply.kz`, point the `target` A record to `154.59.111.195`. Set TTL to 300 seconds or use the provider default. The origin uses IPv4.
2. Install `site.caddy` and `redirect.tmpl` in `/etc/caddy/ai-reply-target/`, readable by Caddy. Add this line outside the existing site block in `/etc/caddy/Caddyfile`.

   ```caddyfile
   import /etc/caddy/ai-reply-target/site.caddy
   ```

3. Validate the merged config with `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile` and run `systemctl reload caddy`. Caddy obtains and renews the certificate using its existing automatic HTTPS configuration. The origin must accept traffic on ports 80 and 443.
4. Check DNS with `dig +short target.ai-reply.kz A`, then verify public HTTPS and the redirect matrix with `python3 ai-reply-target/check-redirect.py`. Check `https://ai-reply.kz/healthz` after the reload. To roll back, remove the campaign import, validate, and reload Caddy.

## Preflight

Run the project on a loopback Caddy listener using the same Caddy version deployed to production. Run these commands from the repository root in a shell with Caddy on `PATH`.

```sh
campaign_dir="$PWD/ai-reply-target"
campaign_tmp=$(mktemp -d)
printf '{\n\tadmin off\n\tdefault_bind 127.0.0.1\n}\nimport %s/site.caddy\n' "$campaign_dir" > "$campaign_tmp/Caddyfile"
CAMPAIGN_ADDRESS=http://target.ai-reply.kz:18081 \
CAMPAIGN_TEMPLATE_ROOT="$campaign_dir" CAMPAIGN_LOG="$campaign_tmp/clicks.json" \
XDG_CONFIG_HOME="$campaign_tmp/config" XDG_DATA_HOME="$campaign_tmp/data" \
caddy run --config "$campaign_tmp/Caddyfile" --adapter caddyfile
```

In another shell, run `python3 ai-reply-target/check-redirect.py --url https://target.ai-reply.kz:18081/ --origin 127.0.0.1:18081`.

## Campaign analytics

Example campaign URL with a source, medium, and campaign name.

```text
https://target.ai-reply.kz/?utm_source=instagram&utm_medium=paid_social&utm_campaign=launch
```

The redirect carries the standard UTM fields shown in `redirect.tmpl`. Caddy writes rotating JSON logs at `/var/log/caddy/ai-reply-target.json`. Read `ts`, `status`, and `resp_headers.Location[0]` for click time, redirect status, destination platform, and campaign parameters. The logs omit client IPs, request headers, and raw URLs. Known previews, HEAD, and prefetch requests are excluded. The metric counts redirect requests and can include repeat visits. Store-install attribution uses the respective app store's campaign reporting.
