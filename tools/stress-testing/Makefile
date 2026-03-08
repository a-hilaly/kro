.PHONY: build test dashboard-apply

ROOT := ../..
DASHBOARD_JSON := dashboards/kro-controller-metrics.json
DASHBOARD_CONFIGMAP := kro-controller-metrics-dashboard

build:
	cd $(ROOT) && go build -o bin/krostress ./tools/stress-testing/cmd/krostress

test:
	cd $(ROOT) && go test ./tools/stress-testing/...

dashboard-apply:
	kubectl -n monitoring create configmap $(DASHBOARD_CONFIGMAP) \
		--from-file=kro-controller-metrics.json=$(DASHBOARD_JSON) \
		--dry-run=client -o yaml | \
		kubectl label --local -f - grafana_dashboard=1 -o yaml | \
		kubectl apply -f -
