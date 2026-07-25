package serialize

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"go.yaml.in/yaml/v3"
)

// YAML writes a deterministic YAML 1.2-compatible representation of the JSON
// semantic model. It does not claim to preserve source comments, anchors, or
// scalar styles that are absent from the semantic model.
func YAML(
	ctx context.Context,
	writer io.Writer,
	source Source,
	options Options,
) error {
	if ctx == nil {
		return errors.New("serialize YAML: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if writerIsNil(writer) {
		return errors.New("serialize YAML: nil writer")
	}
	if sourceIsNil(source) {
		return errors.New("serialize YAML: nil source")
	}
	if options.Mode != Preserve && options.Mode != Canonical {
		return fmt.Errorf("serialize YAML: invalid mode %d", options.Mode)
	}
	if options.MaxBytes < 1 || options.MaxDepth < 1 || options.MaxNodes < 1 {
		return fmt.Errorf("%w: limits must be positive", ErrLimitExceeded)
	}
	builder := yamlBuilder{ctx: ctx, options: options, remainingNodes: options.MaxNodes}
	root, err := builder.node(source.Raw(), 0)
	if err != nil {
		return fmt.Errorf("serialize YAML: %w", err)
	}
	bounded := &boundedContextWriter{
		ctx:       ctx,
		writer:    writer,
		remaining: options.MaxBytes,
	}
	encoder := yaml.NewEncoder(bounded)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		// yaml.v3 can only fail for the valid node graph above when its
		// writer fails; bounded records that original classified error.
		return fmt.Errorf("serialize YAML: %w", bounded.failure)
	}
	// Encode fully emits and flushes a single valid document. Close only
	// releases the already-finished yaml.v3 emitter in this code path.
	_ = encoder.Close()
	return nil
}

type yamlBuilder struct {
	ctx            context.Context
	options        Options
	remainingNodes int
}

func (builder *yamlBuilder) node(
	value jsonvalue.Value,
	depth int,
) (*yaml.Node, error) {
	if err := builder.ctx.Err(); err != nil {
		return nil, err
	}
	if builder.remainingNodes == 0 {
		return nil, fmt.Errorf("%w: semantic nodes", ErrLimitExceeded)
	}
	builder.remainingNodes--
	childCount, _ := value.Length()
	if !serializationChildrenFit(
		childCount, depth, builder.remainingNodes, builder.options.MaxDepth,
	) {
		return nil, fmt.Errorf("%w: semantic children", ErrLimitExceeded)
	}
	switch value.Kind() {
	case jsonvalue.NullKind:
		return scalarNode("!!null", "null"), nil
	case jsonvalue.BooleanKind:
		boolean, _ := value.Bool()
		if boolean {
			return scalarNode("!!bool", "true"), nil
		}
		return scalarNode("!!bool", "false"), nil
	case jsonvalue.NumberKind:
		number, _ := value.NumberText()
		tag := "!!int"
		if strings.ContainsAny(number, ".eE") {
			tag = "!!float"
		}
		return scalarNode(tag, number), nil
	case jsonvalue.StringKind:
		text, _ := value.Text()
		return scalarNode("!!str", text), nil
	case jsonvalue.ArrayKind:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		elements, _ := value.Elements()
		for _, element := range elements {
			child, err := builder.node(element, depth+1)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case jsonvalue.ObjectKind:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		members, _ := value.Members()
		if builder.options.Mode == Canonical {
			slices.SortStableFunc(members, func(left, right jsonvalue.Member) int {
				return strings.Compare(left.Name, right.Name)
			})
		}
		for _, member := range members {
			child, err := builder.node(member.Value, depth+1)
			if err != nil {
				return nil, err
			}
			node.Content = append(
				node.Content,
				scalarNode("!!str", member.Name),
				child,
			)
		}
		return node, nil
	default:
		return nil, jsonvalue.ErrInvalidValue
	}
}

func scalarNode(tag string, value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

type boundedContextWriter struct {
	ctx       context.Context
	writer    io.Writer
	remaining int
	failure   error
}

func (writer *boundedContextWriter) Write(value []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		writer.failure = err
		return 0, err
	}
	if len(value) > writer.remaining {
		allowed := writer.remaining
		if allowed != 0 {
			if err := writeAll(writer.writer, value[:allowed]); err != nil {
				writer.failure = err
				return 0, err
			}
			writer.remaining = 0
		}
		writer.failure = ErrLimitExceeded
		return allowed, writer.failure
	}
	if err := writeAll(writer.writer, value); err != nil {
		writer.failure = err
		return 0, err
	}
	writer.remaining -= len(value)
	return len(value), nil
}
