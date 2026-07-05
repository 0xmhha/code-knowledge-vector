#!/usr/bin/env bash
#
# build-knowledge.sh — reproducible CKV knowledge-DB build + human-wording
# semantic validation.
#
# Codifies the "consistent build recipe" so every regenerated vector.db carries
# ALL bridge layers (precise code symbols + canonical_id + human domain docs +
# curated flow corpus), then proves the human-wording -> code-keyword bridge by
# running paraphrased Jira-style queries and checking the expected code file is
# retrieved.
#
# Why one script: ad-hoc `ckv build` flags repeatedly dropped a layer (a bare
# build had no canonical_id; pr-77 had no .claude/docs). This pins the full
# recipe + the validation in one place. (flow-ingest plan Phase E + F.)
#
# Usage:
#   scripts/build-knowledge.sh                 # build + verify + validate
#   scripts/build-knowledge.sh --skip-build    # re-validate an existing index
#   scripts/build-knowledge.sh --build-only    # build + verify, no query eval
#
# Override any path via env (defaults target the pr-77-2 / go-stablenet@0bf2f4d1b
# dataset on this machine):
#   SRC, CKG_DIR, FILTER, DOCS, FLOW_CORPUS, OUT, EMBEDDER, MODEL, LANGS, QUERIES
#
set -uo pipefail

# ---- config (env-overridable) ----------------------------------------------
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${SRC:-/Users/wm-it-25_0220/Work/github/test/analysis-test-3}"
CKG_DIR="${CKG_DIR:-/Users/wm-it-25_0220/Work/github/knowledge-data/pr-77-2}"
FILTER="${FILTER:-/Users/wm-it-25_0220/Work/github/code-knowledge-graph/eval/stablenet/stablenet-files-with-tests.json}"
DOCS="${DOCS:-$SRC/.claude/docs}"
FLOW_CORPUS="${FLOW_CORPUS:-/Users/wm-it-25_0220/Work/github/go-stablenet/.claude.backup.20260625_180533/docs/corpus/corpus.jsonl}"
POLICY="${POLICY:-$REPO/policy/stablenet.yaml}"
OUT="${OUT:-$CKG_DIR/vector-db}"
EMBEDDER="${EMBEDDER:-ollama}"
MODEL="${MODEL:-bge-m3}"
LANGS="${LANGS:-go,solidity}"
OLLAMA="${CKV_OLLAMA_ENDPOINT:-http://127.0.0.1:11434}"
QUERIES="${QUERIES:-$REPO/scripts/semantic-validation-queries.json}"
CKV="$REPO/bin/ckv"

SKIP_BUILD=0; BUILD_ONLY=0
for a in "$@"; do
  case "$a" in
    --skip-build) SKIP_BUILD=1 ;;
    --build-only) BUILD_ONLY=1 ;;
    *) echo "unknown arg: $a" >&2; exit 2 ;;
  esac
done

say() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
die() { printf '\033[31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }

# ---- preflight --------------------------------------------------------------
preflight() {
  say "preflight"
  [ -d "$SRC" ] || die "SRC not found: $SRC"
  [ -f "$CKG_DIR/graph.db" ] || die "ckg graph.db not found in: $CKG_DIR"
  [ -f "$FILTER" ] || die "files-from filter not found: $FILTER"
  [ -f "$FLOW_CORPUS" ] || echo "  warn: flow corpus missing ($FLOW_CORPUS) — building without it"
  [ -d "$DOCS" ] || echo "  warn: docs dir missing ($DOCS) — building without it"

  # ollama reachable + model present (only when using the ollama embedder)
  if [ "$EMBEDDER" = "ollama" ]; then
    curl -s "$OLLAMA/api/tags" 2>/dev/null | grep -q "$MODEL" \
      || die "ollama model '$MODEL' not available at $OLLAMA (run: ollama pull $MODEL)"
  fi

  # ckg schema must be >= 1.19 for canonical_id to be populated (ADR-007 / D-2)
  local sv; sv="$(sqlite3 "$CKG_DIR/graph.db" "SELECT value FROM manifest WHERE key='schema_version';" 2>/dev/null)"
  local commit; commit="$(sqlite3 "$CKG_DIR/graph.db" "SELECT value FROM manifest WHERE key='src_commit';" 2>/dev/null)"
  echo "  ckg: schema_version=$sv  src_commit=${commit:0:12}"
  case "$sv" in
    1.19|1.2[0-9]|1.[3-9][0-9]|[2-9].*) : ;;  # >= 1.19 (coarse guard; ckgalign does the precise gate)
    *) echo "  warn: ckg schema_version=$sv may be < 1.19 — canonical_id could be empty" ;;
  esac
  echo "  recipe: src=$SRC"
  echo "          ckg=$CKG_DIR  filter=$(basename "$FILTER")  langs=$LANGS"
  echo "          docs=$DOCS"
  echo "          flow=$FLOW_CORPUS"
  echo "          policy=$POLICY"
  echo "          embedder=$EMBEDDER/$MODEL  out=$OUT"
}

# ---- build (full recipe) ----------------------------------------------------
build() {
  say "build (full recipe)"
  [ -x "$CKV" ] || { echo "  building bin/ckv"; (cd "$REPO" && make build >/dev/null) || die "make build"; }
  local args=(build --src="$SRC" --ckg="$CKG_DIR" --files-from="$FILTER" --out="$OUT" --lang="$LANGS")
  [ -d "$DOCS" ] && args+=(--docs="$DOCS")
  [ -f "$FLOW_CORPUS" ] && args+=(--flow-corpus="$FLOW_CORPUS")
  [ -f "$POLICY" ] && args+=(--policy="$POLICY")
  rm -rf "$OUT"
  echo "  ckv ${args[*]} --embedder $EMBEDDER --model-name $MODEL"
  CKV_OLLAMA_ENDPOINT="$OLLAMA" "$CKV" "${args[@]}" --embedder "$EMBEDDER" --model-name "$MODEL" \
    || die "ckv build"
  [ -f "$OUT/manifest.json" ] || die "no manifest.json — build did not complete"
}

# ---- verify: manifest + canonical_id match rate -----------------------------
verify() {
  say "verify"
  local db="$OUT/vector.db"
  python3 - "$OUT/manifest.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
print(f"  embedder={d['embedding_model']} dim={d['embedding_dim']} chunks={d['chunk_count']} commit={d['src_commit'][:12]} langs={d['languages']}")
PY
  echo "  canonical_id match rate (go/sol symbol chunks):"
  sqlite3 "$db" "
    SELECT printf('    %d / %d = %.4f',
      SUM(chunk_kind='symbol' AND language IN ('go','solidity') AND canonical_id!=''),
      SUM(chunk_kind='symbol' AND language IN ('go','solidity')),
      1.0*SUM(chunk_kind='symbol' AND language IN ('go','solidity') AND canonical_id!='')
        / NULLIF(SUM(chunk_kind='symbol' AND language IN ('go','solidity')),0))
    FROM chunks;"
  echo "  flow-corpus chunks: $(sqlite3 "$db" "SELECT COUNT(*) FROM chunks WHERE chunk_kind IN ('flow_step','flow_spine') OR provenance='curated';")"
  echo "  doc chunks: $(sqlite3 "$db" "SELECT COUNT(*) FROM chunks WHERE chunk_kind='doc';")"
}

# ---- semantic validation: human-wording -> code bridge ----------------------
validate() {
  say "semantic validation (human wording -> code keyword)"
  [ -f "$QUERIES" ] || die "query set not found: $QUERIES"
  local k; k="$(python3 -c "import json;print(json.load(open('$QUERIES')).get('k',10))")"
  local pass=0 total=0
  while IFS=$'\t' read -r q expect note; do
    total=$((total+1))
    local hits; hits="$(CKV_OLLAMA_ENDPOINT="$OLLAMA" "$CKV" query "$q" --out "$OUT" \
      --embedder "$EMBEDDER" --model-name "$MODEL" -k "$k" --threshold -1 --json --no-footprint 2>/dev/null \
      | python3 -c "import json,sys
try: d=json.load(sys.stdin)
except: print(''); sys.exit()
print('\n'.join(h.get('citation',{}).get('file','') for h in d.get('hits',[])))")"
    if echo "$hits" | grep -q "$expect"; then
      local rank; rank="$(echo "$hits" | grep -n "$expect" | head -1 | cut -d: -f1)"
      printf '  \033[32mPASS\033[0m [rank %s] %s\n        → %s\n' "$rank" "$note" "$q"
      pass=$((pass+1))
    else
      printf '  \033[31mMISS\033[0m %s (expect %s)\n        → %s\n        top: %s\n' "$note" "$expect" "$q" "$(echo "$hits" | head -3 | tr '\n' ' ')"
    fi
  done < <(python3 -c "
import json
d=json.load(open('$QUERIES'))
for e in d['queries']:
    print(e['query']+'\t'+e['expect']+'\t'+e.get('note',''))
")
  echo ""
  echo "  bridge pass rate: $pass / $total"
  [ "$pass" -eq "$total" ] && echo "  ✅ all queries resolved to expected code" || echo "  ⚠️ some queries missed (see MISS above)"
}

# ---- publish: path + sha ----------------------------------------------------
publish() {
  say "publish"
  sqlite3 "$OUT/vector.db" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1
  local sha; sha="$(shasum -a 256 "$OUT/vector.db" | awk '{print $1}')"
  echo "  vector.db : $OUT/vector.db"
  echo "  sha256    : $sha"
}

# ---- main -------------------------------------------------------------------
preflight
[ "$SKIP_BUILD" -eq 1 ] || build
verify
[ "$BUILD_ONLY" -eq 1 ] || validate
publish
say "done"
