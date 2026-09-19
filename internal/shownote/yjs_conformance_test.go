package shownote

import (
	"encoding/base64"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
)

// This fixture is produced by yjs@13.6.30 using a Y.Text named markdown. It
// deliberately includes emoji (two UTF-16 code units) and an en dash.
const javascriptYjsState = "AQGy8hkABAEIbWFya2Rvd248IyBDb2xkIPCfmIAgb3BlbgoKLSBbQ2xpcF0ocmV3aW5kOi8vY2xpcC9hYmMpIEAgMDoxOOKAkzA6NDIKAA=="
const javascriptRelativePosition = "ALLyGQcA"

func TestJavaScriptYjsStateAndRelativePositionConformance(t *testing.T) {
	state, err := base64.StdEncoding.DecodeString(javascriptYjsState)
	require.NoError(t, err)
	doc := crdt.New(crdt.WithClientID(9001))
	require.NoError(t, crdt.ApplyUpdateV1(doc, state, nil))
	require.Equal(t, "# Cold 😀 open\n\n- [Clip](rewind://clip/abc) @ 0:18–0:42\n", doc.GetText("markdown").ToString())

	encodedPosition, err := base64.StdEncoding.DecodeString(javascriptRelativePosition)
	require.NoError(t, err)
	position, err := crdt.DecodeRelativePosition(encodedPosition)
	require.NoError(t, err)
	absolute, ok := crdt.ToAbsolutePosition(doc, position)
	require.True(t, ok)
	require.Equal(t, "markdown", absolute.Name)
	require.Equal(t, 7, absolute.Index)

	markdown := doc.GetText("markdown")
	doc.Transact(func(txn *crdt.Transaction) {
		markdown.Insert(txn, absolute.Index, "shared ", nil)
	})
	goUpdate := crdt.EncodeStateAsUpdateV1(doc, nil)
	replica := crdt.New()
	require.NoError(t, crdt.ApplyUpdateV1(replica, goUpdate, nil))
	require.Equal(t, doc.GetText("markdown").ToString(), replica.GetText("markdown").ToString())
}
