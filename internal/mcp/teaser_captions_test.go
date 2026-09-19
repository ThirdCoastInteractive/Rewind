package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTeaserCaptionToolIsRegistered(t *testing.T) {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "rewind", Version: "test"}, nil)
	registerTeaserCaptionTools(srv, nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "style_teaser_captions" {
			return
		}
	}
	t.Fatal("style_teaser_captions was not registered")
}
