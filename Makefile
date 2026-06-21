templ:
	go tool templ generate -v

tailwind: templ
	tailwindcss -i ./assets/css/input.css -o ./assets/css/output.css

minify-js:
	npx -y esbuild internal/js/videosync.js --minify --outfile=assets/js/videosync.min.js

generate: templ tailwind minify-js

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

docker-build: generate
	docker build --platform linux/amd64,linux/arm64 .
