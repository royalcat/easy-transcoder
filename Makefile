templ:
	go tool templ generate -v

tailwind: templ
	tailwindcss -i ./assets/css/input.css -o ./assets/css/output.css

generate: templ tailwind
	
build-dir:
	mkdir -p build

bin: build-dir generate
	go build -o build/easy-transcoder ./cmd/easy-transcoder

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