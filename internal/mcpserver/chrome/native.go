package chrome

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

// RunNativeHost implements the Chrome Native Messaging host protocol.
// It reads length-prefixed JSON from stdin and proxies it to the UDS,
// and vice-versa.
func RunNativeHost(ctx context.Context, _ zerolog.Logger) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "clara")
	udsPath := filepath.Join(dataDir, "chrome-bridge.sock")

	// Debug log — written to a file so we can inspect it even when Chrome
	// captures stderr. Truncated on each fresh launch.
	debugPath := filepath.Join(dataDir, "chrome-native-host.log")
	debugFile, _ := os.OpenFile(debugPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	dlog := zerolog.New(zerolog.MultiLevelWriter(os.Stderr, debugFile)).
		With().Timestamp().Logger()

	dlog.Info().Str("uds", udsPath).Msg("native host started")

	// 1. Connect to UDS
	var conn net.Conn
	var err error
	for i := 0; i < 5; i++ {
		conn, err = net.Dial("unix", udsPath)
		if err == nil {
			break
		}
		dlog.Warn().Err(err).Int("attempt", i+1).Msg("dial UDS failed, retrying")
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		dlog.Error().Err(err).Msg("could not connect to bridge UDS — is clara mcp chrome running?")
		return errors.Wrap(err, "dial chrome bridge UDS")
	}
	dlog.Info().Msg("connected to bridge UDS")
	defer conn.Close()

	errCh := make(chan error, 2)

	// 2. Chrome (stdin) -> UDS
	go func() {
		for {
			var length uint32
			if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
				dlog.Info().Err(err).Msg("stdin closed")
				errCh <- err
				return
			}
			msg := make([]byte, length)
			if _, err := io.ReadFull(os.Stdin, msg); err != nil {
				dlog.Error().Err(err).Msg("stdin read body failed")
				errCh <- err
				return
			}
			dlog.Debug().Int("bytes", len(msg)).Msg("chrome→uds")
			if _, err := conn.Write(append(msg, '\n')); err != nil {
				dlog.Error().Err(err).Msg("UDS write failed")
				errCh <- err
				return
			}
		}
	}()

	// 3. UDS -> Chrome (stdout)
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var msg json.RawMessage
			if err := decoder.Decode(&msg); err != nil {
				dlog.Info().Err(err).Msg("UDS closed")
				errCh <- err
				return
			}
			dlog.Debug().Int("bytes", len(msg)).Msg("uds→chrome")
			if err := binary.Write(os.Stdout, binary.LittleEndian, uint32(len(msg))); err != nil {
				dlog.Error().Err(err).Msg("stdout write length failed")
				errCh <- err
				return
			}
			if _, err := os.Stdout.Write(msg); err != nil {
				dlog.Error().Err(err).Msg("stdout write body failed")
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		dlog.Info().Msg("context cancelled")
		return nil
	case err := <-errCh:
		if errors.Is(err, io.EOF) {
			dlog.Info().Msg("EOF — clean disconnect")
			return nil
		}
		dlog.Error().Err(err).Msg("native host exiting with error")
		return err
	}
}
