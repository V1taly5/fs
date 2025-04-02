.PHONY: app disc-mgr migrate migrate-up migrate-down migrate-rollback migrate-to migrate-version gen-proto

# Переменная для пути к файлу миграции
MAIN_MIGRATE = cmd/migrate/main.go

# Переменная для команды запуска миграции
MIGRATE = go run $(MAIN_MIGRATE)

app:
	go run cmd/app.go -config=./.env
disc-mgr:
	go run cmd/discover_manager/main.go -config=./.env
migrate:
	migrate create -ext sql -dir ./migrations -seq ($name)
migrate-up:
	$(MIGRATE) -direction up
migrate-down:
	$(MIGRATE) -direction down
migrate-rollback:
	$(MIGRATE) -direction rollback -steps 2
migrate-to:
	$(MIGRATE) -direction to -version 3
migrate-version:
	$(MIGRATE) -direction version
gen-proto:
	protoc -I=./proto --go_out=./proto/gen/go/ --go_opt=paths=source_relative fs.proto