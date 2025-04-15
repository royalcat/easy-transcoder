templ:
	go tool templ generate -v

tailwind:
	tailwindcss -i ./assets/css/input.css -o ./assets/css/output.css

build-dir:
	mkdir -p build

bin: build-dir templ tailwind
	go build -o build/easy-transcoder ./cmd/easy-transcoder

build: bin templ tailwind

dev-server:
	air \
		--root "./bin" \
		--build.cmd "make -j3 build" \
		--build.bin "build/easy-transcoder" \
		--build.delay "100" \
		--build.include_ext "go" \
		--build.include_ext "templ" \
		--build.stop_on_error "false" \
		--misc.clean_on_exit true