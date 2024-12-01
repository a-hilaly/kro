# View manifest
tar tvf image.tar  # List contents
tar xfO image.tar manifest.json | jq .

# View config
CONFIG=$(tar xfO image.tar manifest.json | jq -r '.config.digest' | sed 's/^sha256://')
tar xfO image.tar "$CONFIG" | jq .

# View ResourceGroup YAML
LAYER=$(tar xfO image.tar manifest.json | jq -r '.layers[0].digest' | sed 's/^sha256://')
tar xfO image.tar "$LAYER" | tar xO resourcegroup.yaml