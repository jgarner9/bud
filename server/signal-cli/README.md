# server/signal-cli/

Signal for Hermes, the manager (Suite D30): signal-cli's daemon behind a systemd user unit, registered on the spare number as a dedicated bot account. Hermes's Signal adapter talks to it on loopback, and bud's `bud-signal` plugin lets a reaction or a code answer an approval. How it works is in [docs/internals/signal.md](https://github.com/jgarner9/bud/blob/main/docs/internals/signal.md).

| File | What it is |
| --- | --- |
| `bud-signal-cli.service` | The user unit. It runs `signal-cli daemon --http 127.0.0.1:8793` for the account registered on this machine. |

## Setting it up

1. Install signal-cli's native Linux build (no Java needed) from [its releases](https://github.com/AsamK/signal-cli/releases): download `signal-cli-<version>-Linux-native.tar.gz`, check it against the release's checksum, and put the `signal-cli` binary at `~/.local/bin/signal-cli`. bud was checked against 0.14.10.
2. Register the bot number, which must be able to receive an SMS: `signal-cli -a +1BOTNUMBER register`. Signal usually asks for a captcha first: solve one at https://signalcaptchas.org/registration/generate.html, copy the "Open Signal" link, and run `signal-cli -a +1BOTNUMBER register --captcha 'signalcaptcha://...'`. Then `signal-cli -a +1BOTNUMBER verify <code from the SMS>`.
3. Set a registration-lock PIN, so nobody else can register the number, and save it in the Bud vault (D30): `signal-cli -a +1BOTNUMBER setPin <PIN>`. Give the bot a name: `signal-cli -a +1BOTNUMBER updateProfile --given-name Bud`.
4. Install and start the unit with the commands at the top of `bud-signal-cli.service` (`loginctl enable-linger "$USER"` is already on for Hermes). Check it: `curl -s http://127.0.0.1:8793/api/v1/check` prints signal-cli's version.
5. Tell Hermes the numbers. They're personal, so they go in `~/.hermes/.env` (or as `op://` references under `bud.hermes.secrets.onepassword.env` in your overlay's env), never in this repo:

   ```sh
   SIGNAL_ACCOUNT=+1BOTNUMBER
   SIGNAL_ALLOWED_USERS=+1YOURNUMBER
   SIGNAL_HOME_CHANNEL=+1YOURNUMBER
   ```

   `SIGNAL_ALLOWED_USERS` must name your number alone: with it unset, or set to `*`, no reaction or code answers an approval. Leave `SIGNAL_GROUP_ALLOWED_USERS` unset, so groups stay off.
6. `bud sync` installs the `bud-signal` plugin in `~/.hermes/plugins/` and renders Signal into Hermes's config (the URL above, unknown senders ignored, the plugin enabled). Then `hermes gateway restart`.
7. Check it end to end, from your phone: send the bot "hi" and get an answer; ask it for something that needs approval (say, a `git push`), react 👍 to the prompt, and see it run and the bot mark the prompt ✅; ask again, reply `no <code>`, and see it refused. A message from any other number gets no answer at all.
