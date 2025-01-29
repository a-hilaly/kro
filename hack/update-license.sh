#!/bin/bash
set -e

OLD_COPYRIGHT="$1"
NEW_COPYRIGHT="// Copyright 2025 The Kube Resource Orchestrator Authors."

# Find all .go files in the specified directories
find ./cmd ./pkg ./test ./hack -name "*.go" -type f | while read -r file; do
    if grep -q "$OLD_COPYRIGHT" "$file"; then
        # Create a backup with .bak extension
        cp "$file" "$file.bak"
        
        # Replace the copyright line
        sed "s|$OLD_COPYRIGHT|$NEW_COPYRIGHT|" "$file.bak" > "$file"
        
        echo "Updated copyright in: $file"
        rm "$file.bak"
    fi
done

echo "Copyright update complete!"