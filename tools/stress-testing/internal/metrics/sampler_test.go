package metrics

import (
	"testing"
	"time"
)

func TestSummarizeSamples(t *testing.T) {
	samples := []Sample{
		{
			Timestamp: time.Unix(1, 0).UTC(),
			Values: map[string]float64{
				"cpu_cores": 1,
				"memory":    100,
			},
		},
		{
			Timestamp: time.Unix(2, 0).UTC(),
			Values: map[string]float64{
				"cpu_cores": 3,
				"memory":    50,
			},
		},
	}

	summary := summarize(samples)

	if summary["cpu_cores"].Min != 1 || summary["cpu_cores"].Max != 3 || summary["cpu_cores"].Avg != 2 || summary["cpu_cores"].Last != 3 {
		t.Fatalf("unexpected cpu summary %#v", summary["cpu_cores"])
	}
	if summary["memory"].Min != 50 || summary["memory"].Max != 100 || summary["memory"].Avg != 75 || summary["memory"].Last != 50 {
		t.Fatalf("unexpected memory summary %#v", summary["memory"])
	}
}
