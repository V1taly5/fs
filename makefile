.PHONY: app

app:
	go run cmd/app.go -config=./.env
disc-mgr:
	go run cmd/discover_manager/main.go -config=./.env