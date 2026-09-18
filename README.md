# tictactoe

Networked tic-tac-toe in Go. The server pairs clients and referees the games;
the client serves a small web page to play in a browser.

## Run

Start the server:

```sh
go run ./cmd/server
```

Start a client for each player, each on its own port, and open the printed URL:

```sh
go run ./cmd/client -addr :3000
go run ./cmd/client -addr :3001
```

The first two players to connect are paired; a third waits for a fourth.

### Flags

| Binary | Flag | Default | Meaning |
|---|---|---|---|
| server | `-addr` | `:8080` | listen address |
| server | `-queue-capacity` | `1024` | players that can wait for a match |
| server | `-move-timeout` | `15s` | time per move before a player is dropped |
| client | `-addr` | `:3000` | address to serve the page on |
| client | `-server` | `ws://localhost:8080/ws` | game server websocket URL |

## Test

```sh
go test -race ./...
```

## Not implemented

- Players are not notified when the server shuts down; their connections simply
  end.
- No reconnection: a dropped connection is a new player with a new ID.
