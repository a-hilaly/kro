package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	metricsutil "github.com/kubernetes-sigs/kro/tools/stress-testing/internal/metrics"
)

type StageMarker struct {
	Label       string    `json:"label"`
	Count       int       `json:"count"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description,omitempty"`
}

type RunSummary struct {
	Title           string              `json:"title"`
	Lead            string              `json:"lead"`
	UnitsLabel      string              `json:"unitsLabel"`
	LevelLabel      string              `json:"levelLabel"`
	StartedAt       time.Time           `json:"startedAt"`
	EndedAt         time.Time           `json:"endedAt"`
	Duration        string              `json:"duration"`
	Complexity      string              `json:"complexity"`
	TotalUnits      int                 `json:"totalUnits"`
	StepSize        int                 `json:"stepSize"`
	Rate            int                 `json:"rate"`
	Prefix          string              `json:"prefix"`
	ExistingUnits   int                 `json:"existingUnits"`
	Observation     *metricsutil.Report `json:"observation"`
	Markers         []StageMarker       `json:"markers"`
	StageSnapshots  []StageSnapshot     `json:"stageSnapshots"`
	CPUProfilePath  string              `json:"cpuProfilePath"`
	HeapProfilePath string              `json:"heapProfilePath"`
	CPUHotPaths     string              `json:"cpuHotPaths"`
	HeapHotPaths    string              `json:"heapHotPaths"`
}

type StageSnapshot struct {
	Label     string             `json:"label"`
	Count     int                `json:"count"`
	Timestamp time.Time          `json:"timestamp"`
	Values    map[string]float64 `json:"values"`
}

func BuildStageSnapshots(report *metricsutil.Report, markers []StageMarker) []StageSnapshot {
	if report == nil || len(report.Samples) == 0 || len(markers) == 0 {
		return nil
	}

	snapshots := make([]StageSnapshot, 0, len(markers))
	for _, marker := range markers {
		var (
			best     metricsutil.Sample
			bestDiff time.Duration
			found    bool
		)

		for _, sample := range report.Samples {
			diff := sample.Timestamp.Sub(marker.Timestamp)
			if diff < 0 {
				diff = -diff
			}
			if !found || diff < bestDiff {
				best = sample
				bestDiff = diff
				found = true
			}
		}

		if !found {
			continue
		}

		values := make(map[string]float64, len(best.Values))
		for key, value := range best.Values {
			values[key] = value
		}

		snapshots = append(snapshots, StageSnapshot{
			Label:     marker.Label,
			Count:     marker.Count,
			Timestamp: best.Timestamp,
			Values:    values,
		})
	}

	return snapshots
}

func WriteHTML(path string, summary RunSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}

	observationJSON, err := json.Marshal(summary.Observation)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	markersJSON, err := json.Marshal(summary.Markers)
	if err != nil {
		return fmt.Errorf("marshal markers: %w", err)
	}
	snapshotsJSON, err := json.Marshal(summary.StageSnapshots)
	if err != nil {
		return fmt.Errorf("marshal stage snapshots: %w", err)
	}

	data := struct {
		RunSummary
		ObservationJSON template.JS
		MarkersJSON     template.JS
		SnapshotsJSON   template.JS
	}{
		RunSummary:      summary,
		ObservationJSON: template.JS(string(observationJSON)),
		MarkersJSON:     template.JS(string(markersJSON)),
		SnapshotsJSON:   template.JS(string(snapshotsJSON)),
	}

	tpl, err := template.New("report").Funcs(template.FuncMap{
		"divf": func(v float64, by float64) float64 {
			if by == 0 {
				return 0
			}
			return v / by
		},
	}).Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse HTML template: %w", err)
	}

	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, data); err != nil {
		return fmt.Errorf("render HTML template: %w", err)
	}

	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write HTML report: %w", err)
	}
	return nil
}

const htmlTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{ .Title }}</title>
  <style>
    :root {
      --bg: #f5f1e8;
      --panel: #fffdf8;
      --ink: #1e1b18;
      --muted: #6d655d;
      --accent: #bd5d38;
      --accent-2: #236d5d;
      --grid: #ded5c7;
      --border: #d8ccbc;
      --shadow: 0 14px 32px rgba(30, 27, 24, 0.08);
    }
    body {
      margin: 0;
      padding: 32px;
      background: radial-gradient(circle at top, #fff8eb 0%, var(--bg) 45%, #efe4d5 100%);
      color: var(--ink);
      font-family: Georgia, "Iowan Old Style", serif;
    }
    h1, h2, h3 {
      margin: 0 0 12px;
      font-weight: 700;
    }
    h1 { font-size: 36px; }
    h2 { font-size: 24px; margin-top: 32px; }
    h3 { font-size: 18px; margin-top: 24px; }
    p, li, td, th, pre {
      font-size: 14px;
      line-height: 1.5;
    }
    .lead {
      color: var(--muted);
      max-width: 1000px;
      margin-bottom: 24px;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 16px;
      margin: 24px 0;
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 18px;
      box-shadow: var(--shadow);
    }
    .metric {
      font-size: 28px;
      font-weight: 700;
      margin-top: 6px;
    }
    .label {
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.08em;
      font-size: 11px;
    }
    .chart {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 18px;
      box-shadow: var(--shadow);
      margin-bottom: 20px;
    }
    .chart svg {
      width: 100%;
      height: auto;
      display: block;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      background: var(--panel);
      border-radius: 16px;
      overflow: hidden;
      box-shadow: var(--shadow);
      border: 1px solid var(--border);
    }
    th, td {
      padding: 12px 14px;
      border-bottom: 1px solid var(--border);
      text-align: left;
    }
    th {
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
      background: #faf4ea;
    }
    tr:last-child td { border-bottom: 0; }
    pre {
      background: #201c19;
      color: #f5eee4;
      padding: 16px;
      border-radius: 14px;
      overflow-x: auto;
      box-shadow: var(--shadow);
    }
    .small {
      color: var(--muted);
      font-size: 12px;
    }
  </style>
</head>
<body>
  <h1>{{ .Title }}</h1>
  <p class="lead">
    {{ .Lead }}
  </p>

  <div class="grid">
    <div class="card">
      <div class="label">Run Window</div>
      <div class="metric">{{ .StartedAt.Format "2006-01-02 15:04:05 MST" }}</div>
      <div class="small">to {{ .EndedAt.Format "2006-01-02 15:04:05 MST" }}</div>
    </div>
    <div class="card">
      <div class="label">Duration</div>
      <div class="metric">{{ .Duration }}</div>
      <div class="small">sample count: {{ if .Observation }}{{ len .Observation.Samples }}{{ else }}0{{ end }}</div>
    </div>
    <div class="card">
      <div class="label">{{ .UnitsLabel }}</div>
      <div class="metric">{{ .TotalUnits }}</div>
      <div class="small">step size: {{ .StepSize }} / rate: {{ .Rate }} per sec</div>
    </div>
    <div class="card">
      <div class="label">Complexity</div>
      <div class="metric">{{ .Complexity }}</div>
      <div class="small">prefix: {{ .Prefix }} / existing before run: {{ .ExistingUnits }}</div>
    </div>
  </div>

  <h2>Time Series</h2>
  <div class="chart">
    <h3>CPU Cores Over Time</h3>
    <div id="cpu-over-time"></div>
  </div>
  <div class="chart">
    <h3>Memory Working Set Over Time</h3>
    <div id="memory-over-time"></div>
  </div>
  <div class="chart">
    <h3>Heap In Use Over Time</h3>
    <div id="heap-over-time"></div>
  </div>

  <h2>Per-1000 Summary</h2>
  <div class="chart">
    <h3>CPU At Each {{ .LevelLabel }} Level</h3>
    <div id="cpu-by-stage"></div>
  </div>
  <div class="chart">
    <h3>Memory At Each {{ .LevelLabel }} Level</h3>
    <div id="memory-by-stage"></div>
  </div>

  <h2>Stage Table</h2>
  <table>
    <thead>
      <tr>
        <th>Stage</th>
        <th>Timestamp</th>
        <th>CPU Cores</th>
        <th>Memory Working Set</th>
        <th>Heap In Use</th>
        <th>Goroutines</th>
        <th>Workqueue Depth</th>
      </tr>
    </thead>
    <tbody>
      {{ range .StageSnapshots }}
      <tr>
        <td>{{ .Label }}</td>
        <td>{{ .Timestamp.Format "2006-01-02 15:04:05 MST" }}</td>
        <td>{{ printf "%.3f" (index .Values "cpu_cores") }}</td>
        <td>{{ printf "%.2f MiB" (divf (index .Values "memory_working_set_bytes") 1048576) }}</td>
        <td>{{ printf "%.2f MiB" (divf (index .Values "heap_inuse_bytes") 1048576) }}</td>
        <td>{{ printf "%.0f" (index .Values "go_goroutines") }}</td>
        <td>{{ printf "%.0f" (index .Values "workqueue_depth") }}</td>
      </tr>
      {{ end }}
    </tbody>
  </table>

  <h2>pprof Hot Paths</h2>
  <div class="grid">
    <div>
      <h3>CPU Profile Top</h3>
      <pre>{{ .CPUHotPaths }}</pre>
    </div>
    <div>
      <h3>Heap Profile Top</h3>
      <pre>{{ .HeapHotPaths }}</pre>
    </div>
  </div>

  <h2>Artifacts</h2>
  <ul>
    <li>CPU profile: {{ .CPUProfilePath }}</li>
    <li>Heap profile: {{ .HeapProfilePath }}</li>
  </ul>

  <script>
    const observation = {{ .ObservationJSON }};
    const markers = {{ .MarkersJSON }};
    const snapshots = {{ .SnapshotsJSON }};

    function fmtBytes(bytes) {
      if (bytes >= 1024 * 1024 * 1024) return (bytes / (1024 * 1024 * 1024)).toFixed(2) + " GiB";
      if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(2) + " MiB";
      if (bytes >= 1024) return (bytes / 1024).toFixed(2) + " KiB";
      return bytes.toFixed(0) + " B";
    }

	    function createSVG(width, height) {
	      return "<svg viewBox='0 0 " + width + " " + height + "' xmlns='http://www.w3.org/2000/svg'></svg>";
	    }

    function renderLineChart(id, key, color, formatter) {
      const el = document.getElementById(id);
      const width = 960, height = 360;
      const margin = { top: 24, right: 20, bottom: 38, left: 84 };
      const samples = observation.samples || [];
      if (!samples.length) {
        el.innerHTML = "<p>No samples</p>";
        return;
      }

      const times = samples.map(s => new Date(s.timestamp).getTime());
      const values = samples.map(s => s.values[key] || 0);
      const minTime = Math.min(...times), maxTime = Math.max(...times);
      const maxValue = Math.max(...values, 1);
      const minValue = Math.min(...values, 0);

      const x = t => margin.left + ((t - minTime) / Math.max(maxTime - minTime, 1)) * (width - margin.left - margin.right);
      const y = v => height - margin.bottom - ((v - minValue) / Math.max(maxValue - minValue, 1e-9)) * (height - margin.top - margin.bottom);

	      let grid = "";
	      for (let i = 0; i < 5; i++) {
	        const value = minValue + ((maxValue - minValue) * i / 4);
	        const ypos = y(value);
	        grid += "<line x1='" + margin.left + "' y1='" + ypos + "' x2='" + (width - margin.right) + "' y2='" + ypos + "' stroke='var(--grid)' stroke-width='1'/>";
	        grid += "<text x='" + (margin.left - 10) + "' y='" + (ypos + 4) + "' text-anchor='end' fill='var(--muted)' font-size='11'>" + formatter(value) + "</text>";
	      }

	      const path = values.map((value, index) => (index === 0 ? "M" : "L") + " " + x(times[index]).toFixed(2) + " " + y(value).toFixed(2)).join(" ");
	      let markerLines = "";
	      for (const marker of markers) {
	        const t = new Date(marker.timestamp).getTime();
	        const xpos = x(t);
	        markerLines += "<line x1='" + xpos + "' y1='" + margin.top + "' x2='" + xpos + "' y2='" + (height - margin.bottom) + "' stroke='#8a8178' stroke-dasharray='4 4' />";
	        markerLines += "<text x='" + (xpos + 4) + "' y='" + (margin.top + 12) + "' fill='#8a8178' font-size='11'>" + marker.label + "</text>";
	      }

      const startLabel = new Date(minTime).toLocaleTimeString();
      const endLabel = new Date(maxTime).toLocaleTimeString();

	      el.innerHTML = createSVG(width, height).replace("</svg>",
	        "<rect x='0' y='0' width='" + width + "' height='" + height + "' fill='transparent'/>" +
	        grid +
	        markerLines +
	        "<path d='" + path + "' fill='none' stroke='" + color + "' stroke-width='3' stroke-linejoin='round' stroke-linecap='round'/>" +
	        "<line x1='" + margin.left + "' y1='" + (height - margin.bottom) + "' x2='" + (width - margin.right) + "' y2='" + (height - margin.bottom) + "' stroke='var(--ink)' />" +
	        "<line x1='" + margin.left + "' y1='" + margin.top + "' x2='" + margin.left + "' y2='" + (height - margin.bottom) + "' stroke='var(--ink)' />" +
	        "<text x='" + margin.left + "' y='" + (height - 10) + "' fill='var(--muted)' font-size='11'>" + startLabel + "</text>" +
	        "<text x='" + (width - margin.right) + "' y='" + (height - 10) + "' text-anchor='end' fill='var(--muted)' font-size='11'>" + endLabel + "</text>" +
	        "</svg>");
	    }

    function renderBarChart(id, key, color, formatter) {
      const el = document.getElementById(id);
      const width = 960, height = 320;
      const margin = { top: 24, right: 20, bottom: 48, left: 84 };
      if (!snapshots.length) {
        el.innerHTML = "<p>No stage snapshots</p>";
        return;
      }

      const maxValue = Math.max(...snapshots.map(s => s.values[key] || 0), 1);
      const barWidth = (width - margin.left - margin.right) / snapshots.length * 0.64;
      const gap = (width - margin.left - margin.right) / snapshots.length;

      let bars = "";
	      snapshots.forEach((snapshot, index) => {
	        const value = snapshot.values[key] || 0;
	        const x = margin.left + gap * index + (gap - barWidth) / 2;
	        const barHeight = ((height - margin.top - margin.bottom) * value) / maxValue;
	        const y = height - margin.bottom - barHeight;
	        bars += "<rect x='" + x + "' y='" + y + "' width='" + barWidth + "' height='" + barHeight + "' rx='8' fill='" + color + "' />";
	        bars += "<text x='" + (x + barWidth / 2) + "' y='" + (y - 8) + "' text-anchor='middle' fill='var(--ink)' font-size='11'>" + formatter(value) + "</text>";
	        bars += "<text x='" + (x + barWidth / 2) + "' y='" + (height - margin.bottom + 16) + "' text-anchor='middle' fill='var(--muted)' font-size='11'>" + snapshot.label + "</text>";
	      });

	      let grid = "";
	      for (let i = 0; i < 5; i++) {
	        const value = maxValue * i / 4;
	        const ypos = height - margin.bottom - ((height - margin.top - margin.bottom) * value / maxValue);
	        grid += "<line x1='" + margin.left + "' y1='" + ypos + "' x2='" + (width - margin.right) + "' y2='" + ypos + "' stroke='var(--grid)' stroke-width='1'/>";
	        grid += "<text x='" + (margin.left - 10) + "' y='" + (ypos + 4) + "' text-anchor='end' fill='var(--muted)' font-size='11'>" + formatter(value) + "</text>";
	      }

	      el.innerHTML = createSVG(width, height).replace("</svg>",
	        grid +
	        bars +
	        "<line x1='" + margin.left + "' y1='" + (height - margin.bottom) + "' x2='" + (width - margin.right) + "' y2='" + (height - margin.bottom) + "' stroke='var(--ink)' />" +
	        "<line x1='" + margin.left + "' y1='" + margin.top + "' x2='" + margin.left + "' y2='" + (height - margin.bottom) + "' stroke='var(--ink)' />" +
	        "</svg>");
	    }

    renderLineChart("cpu-over-time", "cpu_cores", "var(--accent)", value => value.toFixed(2));
    renderLineChart("memory-over-time", "memory_working_set_bytes", "var(--accent-2)", value => fmtBytes(value));
    renderLineChart("heap-over-time", "heap_inuse_bytes", "#7b5ea7", value => fmtBytes(value));
    renderBarChart("cpu-by-stage", "cpu_cores", "var(--accent)", value => value.toFixed(2));
    renderBarChart("memory-by-stage", "memory_working_set_bytes", "var(--accent-2)", value => fmtBytes(value));
  </script>
</body>
</html>`
