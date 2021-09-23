package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	fmt.Printf("Starting up Tailscale.")
	if err := startTailscale(context.Background()); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Starting up dummy public server.")
	if err := startPublicDummyServer(); err != nil {
		log.Fatal(err)
	}
}

const (
	stateFilePath = "/app/ts.state"
	socketPath    = "/app/ts.sock"
)

const tsTableQuery = `
CREATE TABLE IF NOT EXISTS "tailscale_data" (
	"id"    serial primary key,
	"state" text not null
);
`

func startTailscale(ctx context.Context) error {
	// The TAILSCALE_AUTHKEY is a required environment variable.
	// Users will see a textfield to add their key on the Heroku deploy dashboard.
	// When quick-deploying from the admin panel, the key value gets set automatically.
	tsAuthKey := os.Getenv("TAILSCALE_AUTHKEY")

	// We ask heroku to startup a "heroku-postgresql" add-on (see app.json).
	// We open up the db here to help us manage our tailscale state file.
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL+"?sslmode=require")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// When the Heroku dyno restarts, all state/local storage gets wiped.
	// To avoid creating new tailscale nodes on each restart (restarts happen fairly frequently on the Heroku free tier),
	// we store the tailscale state file contents in our postgres db in a "tailscale_data" table.
	_, err = db.ExecContext(ctx, tsTableQuery)
	if err != nil {
		return fmt.Errorf("failed to create tailscale_data table: %w", err)
	}

	var tsState string
	// Try to grab our state file contents from the db.
	// It's fine if nothing is found. That means we likely haven't authenticated tailscale for the first time yet.
	err = db.QueryRowContext(ctx, "SELECT state FROM tailscale_data ORDER BY id DESC LIMIT 1").Scan(&tsState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read state file from database: %w", err)
	}
	if tsState != "" {
		// If we do have state already, write it out to our locally state file.
		// We will pass this filepath as a flag when starting up tailscale below.
		err := os.WriteFile(stateFilePath, []byte(tsState), 0644)
		if err != nil {
			return fmt.Errorf("failed to write state file: %w", err)
		}
	}
	if tsAuthKey == "" && tsState == "" {
		return fmt.Errorf("TAILSCALE_AUTHKEY or state file must be present")
	}

	// Start `tailscaled`.
	daemoncmd := exec.CommandContext(ctx, "/app/tailscaled", "--socket", socketPath, "--state", stateFilePath, "--tun", "userspace-networking")
	if err = daemoncmd.Start(); err != nil {
		return fmt.Errorf("failed to start tailscaled: %w", err)
	}
	time.Sleep(1 * time.Second) // TODO: this is hacky

	// Start `tailscale`.
	args := []string{"--socket", socketPath, "up", "--hostname", "grafana-server"} // TODO: grab the name from env var?
	if tsAuthKey != "" {
		args = append(args, "--authkey", tsAuthKey)
	}
	cmd := exec.CommandContext(ctx, "/app/tailscale", args...)
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("failed to start tailscale: %w", err)
	}

	// Now that we've started up tailscale, store the state file contents back
	// to the db so we can restore them the next time the dyno restarts.
	b, err := os.ReadFile(stateFilePath)
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}
	if string(b) != tsState {
		_, err := db.ExecContext(ctx, "INSERT INTO tailscale_data(state) VALUES($1)", string(b))
		if err != nil {
			return fmt.Errorf("failed to update state file in database: %w", err)
		}
	}

	return nil
}

// startPublicDummyServer starts a go webserver that displays a simple welcome prompt to viewers.
// Heroku requires something to be running at it's public port, otherwise it shuts down the dyno.
// We only want our server acessible over tailscale, though so we don't want to serve that over
// the public Heroku port. Instead, we place this dummy server at the public endpoint.
func startPublicDummyServer() error {
	// Grab the Heroku random public port assigned to our dyno.
	port := os.Getenv("PORT")
	if port == "" {
		return fmt.Errorf("failed to find Heroku public port")
	}

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome! Hello from %s", hostname) // TODO: make this view a little more useful (maybe add instructions for accessing the service)
	})
	http.ListenAndServe(":"+port, publicMux)

	return nil
}
