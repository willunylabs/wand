.PHONY: test race bench fuzz lint soak

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test ./router -run=^$$ -bench=. -benchmem

fuzz:
	go test ./router -run=^$$ -fuzz=FuzzRouter_ -fuzztime=30s

lint:
	golangci-lint run

soak:
	./scripts/soak.sh
