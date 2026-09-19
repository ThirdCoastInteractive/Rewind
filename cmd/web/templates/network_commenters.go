package templates

import (
	"strings"
)

// NetworkCommenter is a sparse commenter node for the network graph JSON.
type NetworkCommenter struct {
	ID            string
	DisplayName   string
	Source        string
	AuthorURL     string
	CommentCount  int64
	ChannelID     string
	Watchlisted   bool
	OpenFlagKinds []string
	MeanSentiment *float64
	MeanToxicity  *float64
}

// NetworkCommenterEdge is a pre-aggregated commenter graph edge.
type NetworkCommenterEdge struct {
	ID              string
	FromChannelID   string
	CommenterID     string
	PeerCommenterID string
	Kind            string
	Weight          float64
	Evidence        string
	VideoID         string
}

func commenterNodeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "commenter:") {
		return id
	}
	return "commenter:" + id
}

func isCommenterEdgeKind(kind string) bool {
	switch kind {
	case "commented_by", "campaign", "style":
		return true
	default:
		return false
	}
}

// networkCommentersDefaultOn is true when the focused node already has a commenter neighbor.
func networkCommentersDefaultOn(links []networkLink, focus string) bool {
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return false
	}
	for _, l := range links {
		if !isCommenterEdgeKind(l.Kind) {
			continue
		}
		if l.Source == focus || l.Target == focus {
			return true
		}
	}
	return false
}

func attachCommentersToGraph(graph *networkGraph, commenters []NetworkCommenter, edges []NetworkCommenterEdge) {
	if graph == nil {
		return
	}
	byID := map[string]int{}
	for i, n := range graph.Nodes {
		byID[n.ID] = i
	}
	for _, c := range commenters {
		id := commenterNodeID(c.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(c.DisplayName)
		if label == "" {
			label = c.ID
		}
		n := networkNode{
			ID:           id,
			Label:        label,
			Platform:     c.Source,
			Archived:     c.ChannelID != "",
			Type:         "commenter",
			Href:         "/investigate/commenters/" + c.ID,
			Search:       strings.TrimSpace(c.DisplayName + " " + c.Source + " " + c.AuthorURL + " " + strings.Join(c.OpenFlagKinds, " ")),
			CommentCount: c.CommentCount,
			Watchlisted:  c.Watchlisted,
			FlagKinds:    append([]string(nil), c.OpenFlagKinds...),
		}
		if i, ok := byID[id]; ok {
			graph.Nodes[i] = n
		} else {
			byID[id] = len(graph.Nodes)
			graph.Nodes = append(graph.Nodes, n)
		}
	}
	seen := map[string]bool{}
	for _, e := range edges {
		if !isCommenterEdgeKind(e.Kind) {
			continue
		}
		source, target := "", ""
		switch {
		case e.PeerCommenterID != "":
			source = commenterNodeID(e.CommenterID)
			target = commenterNodeID(e.PeerCommenterID)
		case e.FromChannelID != "":
			source = e.FromChannelID
			target = commenterNodeID(e.CommenterID)
		default:
			continue
		}
		if source == "" || target == "" || source == target {
			continue
		}
		if _, ok := byID[target]; !ok && strings.HasPrefix(target, "commenter:") {
			continue
		}
		if _, ok := byID[source]; !ok && !strings.HasPrefix(source, "commenter:") {
			// Channel endpoint missing from the current graph slice; skip.
			continue
		}
		if _, ok := byID[source]; !ok && strings.HasPrefix(source, "commenter:") {
			continue
		}
		edgeID := e.ID
		if edgeID == "" {
			edgeID = e.Kind + ":" + source + ">" + target
		}
		if seen[edgeID] {
			continue
		}
		seen[edgeID] = true
		w := int32(e.Weight)
		if w < 1 {
			w = 1
		}
		graph.Links = append(graph.Links, networkLink{
			ID:       edgeID,
			Source:   source,
			Target:   target,
			Kind:     e.Kind,
			Weight:   w,
			Evidence: e.Evidence,
			VideoID:  e.VideoID,
		})
	}
}
