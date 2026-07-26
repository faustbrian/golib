package kafka_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/kafka"
)

func ExampleReplayResult_Checkpoint() {
	result := kafka.ReplayResult{
		Ranges: []kafka.ReplayRangeResult{{
			ReplayRange: kafka.ReplayRange{
				Topic:       "events",
				Partition:   2,
				StartOffset: 100,
				EndOffset:   200,
			},
			NextOffset: 137,
			Processed:  37,
		}},
	}

	checkpoint := result.Checkpoint()
	position := checkpoint.Positions[0]
	fmt.Printf("%s[%d] resumes at %d\n",
		position.Topic,
		position.Partition,
		position.NextOffset,
	)

	// Output:
	// events[2] resumes at 137
}
