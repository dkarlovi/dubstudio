package commands

import _ "embed"

// embeddedIndexHTML is dub-studio's frontend, vendored into this repo so the
// dubstudio binary is fully self-contained -- no dependency on the Python
// PoC's checkout existing on disk. This file (commands/webassets/index.html)
// is now the canonical copy: edit it here, not in ~/dub-studio/static, which
// is slated for deletion once the PoC is retired entirely. See
// MIGRATION_PLAN.md.
//
//go:embed webassets/index.html
var embeddedIndexHTML []byte
