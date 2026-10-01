module github.com/docmcp/docmcp

go 1.27.0

require (
	github.com/bmatcuk/doublestar/v4 v4.10.2
	github.com/spf13/cobra v1.10.2
	github.com/temoto/robotstxt v1.1.2
	golang.org/x/net v0.59.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/JohannesKaufmann/dom v0.3.1 // indirect
	github.com/JohannesKaufmann/html-to-markdown v1.6.0 // indirect
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/PuerkitoBio/goquery v1.9.2 // indirect
	// Pinned, not floating: chroma-go v0.4.1 depends on chroma-go-local v0.3.4,
	// whose upstream artifacts are gone (its .mod and .info 404 while the checksum
	// database still lists it). Bumping either line breaks `go mod download` with a
	// checksum mismatch. Revisit when upstream republishes.
	github.com/amikos-tech/chroma-go v0.4.0 // indirect
	github.com/amikos-tech/chroma-go-local v0.3.3 // indirect
	github.com/amikos-tech/pure-onnx v0.0.1 // indirect
	github.com/amikos-tech/pure-tokenizers v0.1.5 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/creasty/defaults v1.8.0 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.12 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/modelcontextprotocol/go-sdk v1.8.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.8.6 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
