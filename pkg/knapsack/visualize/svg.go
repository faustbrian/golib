// Package visualize renders bounded deterministic diagnostics from plans that
// first pass independent verification. Rendering is never feasibility proof.
package visualize

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

var (
	// ErrUnverifiedPlan identifies rendering input rejected by the verifier.
	ErrUnverifiedPlan = errors.New("visualize: plan failed verification")
	// ErrRenderLimit identifies coordinates or output geometry beyond the
	// bounded SVG profile.
	ErrRenderLimit = errors.New("visualize: render limit exceeded")
)

const (
	maxRenderCoordinate int64 = 1_000_000
)

// SVG renders a deterministic top-down scene with escaped untrusted labels.
func SVG(request knapsack.NormalizedRequest, plan knapsack.Plan, options verify.Options) (string, error) {
	result := verify.Plan(request, plan, options)
	if !result.Valid() {
		return "", fmt.Errorf("%w: %v", ErrUnverifiedPlan, result.Violations())
	}
	placements := plan.Placements()
	types := make(map[string]knapsack.NormalizedContainer)
	for _, container := range request.Containers() {
		types[container.ID] = container
	}
	var width, height int64 = 10, 1
	for _, instance := range plan.Containers() {
		dimensions := types[instance.TypeID].Dimensions
		if dimensions.X > maxRenderCoordinate || dimensions.Y > maxRenderCoordinate || width > maxRenderCoordinate-dimensions.X-10 {
			return "", ErrRenderLimit
		}
		width += dimensions.X + 10
		if dimensions.Y > height {
			height = dimensions.Y
		}
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`, width, height+20)
	offset := int64(10)
	for _, instance := range plan.Containers() {
		dimensions := types[instance.TypeID].Dimensions
		fmt.Fprintf(&builder, `<g transform="translate(%d 10)"><rect x="0" y="0" width="%d" height="%d" fill="none" stroke="black"/>`, offset, dimensions.X, dimensions.Y)
		for _, placement := range placements {
			if placement.ContainerID != instance.ID {
				continue
			}
			fmt.Fprintf(&builder, `<rect x="%d" y="%d" width="%d" height="%d" fill="#9ecae1" stroke="#2171b5"><title>`, placement.Origin.X, placement.Origin.Y, placement.Dimensions.X, placement.Dimensions.Y)
			var escaped bytes.Buffer
			escapeText(&escaped, placement.ItemID)
			builder.Write(escaped.Bytes())
			builder.WriteString(`</title></rect>`)
		}
		builder.WriteString(`</g>`)
		offset += dimensions.X + 10
	}
	builder.WriteString(`</svg>`)
	return builder.String(), nil
}

func escapeText(destination *bytes.Buffer, value string) {
	for _, character := range value {
		switch character {
		case '&':
			destination.WriteString("&amp;")
		case '<':
			destination.WriteString("&lt;")
		case '>':
			destination.WriteString("&gt;")
		case '"':
			destination.WriteString("&#34;")
		case '\'':
			destination.WriteString("&#39;")
		default:
			destination.WriteRune(character)
		}
	}
}
