Two services to start:

1. Elixir gateway (WebSocket/signaling):

cd /Volumes/DevWorkspace/Basil/omiai
mix deps.get
mix phx.server

Runs on http://localhost:4000.

2. Python API (business logic):

cd /Volumes/DevWorkspace/Basil/omiai-api
source .venv/bin/activate
uvicorn app.main:app --reload --port 8000

Runs on http://localhost:8000.

Then set the env var so the Elixir gateway knows where the Python backend is. Either uncomment the line in config/dev.exs:

config :omiai, :backend_url, "http://localhost:8000"

Or export it before starting Elixir:

OMIAI_BACKEND_URL=http://localhost:8000 mix phx.server


