package msg

import (
	"encoding/json"
	"testing"

	"github.com/matryer/is"
	"github.com/nats-io/nats-server/v2/server"
	stand "github.com/nats-io/nats-streaming-server/server"
	"github.com/nats-io/nats.go"
)

func NewNATSServer(t *testing.T) (*server.Server, func()) {
	// https://sourcegraph.com/github.com/nats-io/nats-server@6da5d2f4907a03c8ba26fc8b6ca2aed903ac80f8/-/blob/main.go
	// Now we want to setup the monitoring port for NATS Streaming.
	// We still need NATS Options to do so, so create NATS Options
	// using the NewNATSOptions() from the streaming server package.
	snopts := stand.NewNATSOptions()
	snopts.Port = nats.DefaultPort
	snopts.HTTPPort = 8223

	// Now run the server with the streaming and streaming/nats options.
	natsServer, err := server.NewServer(snopts)
	if err != nil {
		panic(err)
	}
	// go func() {
	// 	natsServer.Start()
	// 	natsServer.WaitForShutdown()
	// }()

	return natsServer, func() {
		natsServer.Shutdown()
	}
}

func TestCall(t *testing.T) {
	is := is.New(t)
	is.True(true == true)

	_, err := queryAPI("Covid")
	is.NoErr(err)

	// t.Log(string(results))
}

// func TestGetStruct(t *testing.T) {
// 	is := is.New(t)

// 	payload, err := queryAPI("Covid")
// 	is.NoErr(err)

// 	_, err = ToResultSet(payload)
// 	is.NoErr(err)

// 	// t.Logf("%+v", rs)
// }

func TestMessage(t *testing.T) {
	is := is.New(t)
	server, shutdown := NewNATSServer(t)
	server.Start()

	t.Log("address", server.Addr())
	defer shutdown()

	result, err := QueryNATS("Covid")
	is.NoErr(err)

	t.Logf("Got message %+v", string(result))
}

func TestToHTML(t *testing.T) {
	is := is.New(t)
	server, shutdown := NewNATSServer(t)
	server.Start()

	t.Log("address", server.Addr())
	defer shutdown()

	result, err := QueryNATS("Covid")
	is.NoErr(err)

	// t.Log("Query results", string(result))

	results := ResultSet{}
	err = json.Unmarshal(result, &results)
	is.NoErr(err)

	html, err := ToHTML(&results, false)

	t.Log(html)
}
