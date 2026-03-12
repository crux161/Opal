# Opal

Opal is a Charm-based TUI peer client for the Omiai signaling stack. It uses:

- `omiai-api` for signup/login and JWT issuance
- `omiai` Phoenix channels for lobby presence and `relay_message`
- the existing Gem/Omiai Phoenix wire contract for websocket framing and startup registration

## Current scope

This workspace now provides:

- login, signup, and direct-connect flows
- live peer list via `lobby:sankaku`
- per-peer chat over `relay_message`
- typing indicators relayed through the same signaling channel
- saved JWT session reuse between launches

Messages are intentionally server-relayed and ephemeral. This is a basic chat peer, not a durable inbox.

## Run

Start the sibling services first:

```bash
cd /Volumes/DevWorkspace/Mint/Opal/thirdparty/omiai
mix phx.server
```

```bash
cd /Volumes/DevWorkspace/Mint/Opal/thirdparty/omiai-api
uvicorn app.main:app --reload --port 8000
```

Then run Opal:

```bash
cd /Volumes/DevWorkspace/Mint/Opal
go run ./cmd/opal
```

Defaults:

- API: `http://127.0.0.1:8000`
- Signal: `ws://127.0.0.1:4000/ws/sankaku/websocket`

Override them if needed:

```bash
go run ./cmd/opal --api-url http://localhost:8000 --signal-url ws://localhost:4000/ws/sankaku/websocket
```

## Controls

- `left/right`: switch auth mode
- `tab`: move between auth fields or switch chat focus
- `enter`: submit auth, select compose, or send a message
- `up/down`: move through peers
- `ctrl+l`: logout and clear the saved JWT session
- `r`: reconnect after a disconnect
- `q`: quit

## Notes

- Direct mode skips `omiai-api` and authenticates with only a `quicdial_id`, which is useful for local signaling-only testing.
- The websocket client follows the same Phoenix frame shape Gem already uses in `pkg/webrtc_shim`; this keeps Opal aligned with the existing Omiai signaling contract instead of inventing another one.
