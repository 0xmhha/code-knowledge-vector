#!/usr/bin/env bash
#
# pr-retrieval-eval.sh — bias-free PR-based retrieval eval.
#
# For each real go-stablenet PR in testdata/prs.yaml: query = the PR's own
# title+body (human wording, fetched from GitHub), ground truth = the files that
# PR actually changed (objective), index = a code-only bge-m3 build at the PR's
# base_sha (the "before the fix" world; no leakage). Reports file-level
# recall@5/@10 + MRR. Neither the query nor the ground truth is author-picked,
# so it measures the retrieval bridge without teaching-to-the-test bias.
# See docs/pr-retrieval-eval-2026-07-08.md.
#
# Design note: gh is called ONCE up front (prefetch + cache), then the long
# per-PR build loop runs fully offline — interleaving gh with ~20-min builds
# over hours makes the run fragile to transient gh failures.
#
# Requires: gh (authenticated), ollama serving bge-m3, a go-stablenet clone with
# every base_sha reachable, and the CKG file filter. Env-overridable:
#   GS, PRS, FILTER, OLLAMA, MODEL
set -uo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Machine-local paths from build-profiles.env (git-ignored); see
# build-profiles.env.example. Every value stays env-overridable.
if [ -f "$REPO/build-profiles.env" ]; then
  # shellcheck disable=SC1091
  . "$REPO/build-profiles.env"
fi
GS="${GS:-}"
PRS="${PRS:-$REPO/testdata/prs.yaml}"
FILTER="${FILTER:-}"
REPONAME="${REPONAME:-stable-net/go-stablenet}"
[ -n "$GS" ] && [ -n "$FILTER" ] \
  || { echo "GS and FILTER must be set — copy build-profiles.env.example to build-profiles.env and edit (or pass via env)" >&2; exit 2; }
MODEL="${MODEL:-bge-m3}"
OLLAMA="${CKV_OLLAMA_ENDPOINT:-http://127.0.0.1:11434}"
CKV="$REPO/bin/ckv"
W="${W:-$(mktemp -d)}"          # work dir (meta cache, worktrees, indexes)
TSV="$W/pr-eval-results.tsv"
export CKV_OLLAMA_ENDPOINT="$OLLAMA"

say(){ printf '\n\033[1m== %s\033[0m\n' "$*"; }
[ -x "$CKV" ] || (cd "$REPO" && make build >/dev/null) || { echo "make build failed"; exit 1; }

# pr -> base_sha from prs.yaml
python3 - "$PRS" > "$W/pr_bases.txt" <<'PY'
import re,sys
txt=open(sys.argv[1]).read()
for b in re.split(r'\n(?=  - id:)', txt):
    mid=re.search(r'id:\s*(pr\d+)', b); ms=re.search(r'base_sha:\s*([0-9a-f]{40})', b)
    if mid and ms: print(mid.group(1), ms.group(1))
PY

# 1) prefetch gh metadata (query + changed files), with retry
say "prefetch gh metadata"
while read pr sha; do
  n="${pr#pr}"
  for t in 1 2 3; do
    gh pr view "$n" --repo "$REPONAME" --json title,body,files > "$W/meta-$pr.json" 2>/dev/null
    [ -s "$W/meta-$pr.json" ] && python3 -c "import json;json.load(open('$W/meta-$pr.json'),strict=False)" 2>/dev/null && break
    sleep 4
  done
  [ -s "$W/meta-$pr.json" ] && echo "  $pr ok" || echo "  $pr FETCH FAIL"
done < "$W/pr_bases.txt"

# 2) offline build + measure loop (no gh)
say "build + measure (offline)"
echo -e "pr\tn_gt\trecall@5\trecall@10\tmrr\tgt_ranks" > "$TSV"
while read pr sha; do
  meta="$W/meta-$pr.json"; [ -s "$meta" ] || { echo "$pr: no meta"; continue; }
  python3 - "$meta" "$W/gt-$pr.txt" "$W/q-$pr.txt" <<'PY'
import json,re,sys
d=json.load(open(sys.argv[1]),strict=False)
gt=[f['path'] for f in d.get('files',[]) if f['path'].endswith(('.go','.sol')) and not f['path'].endswith('_test.go') and '/test/' not in f['path']]
open(sys.argv[2],'w').write('\n'.join(gt))
b=re.sub(r'\s+',' ',re.sub(r'[^0-9A-Za-z가-힣 ]',' ',(d.get('body') or '')))
open(sys.argv[3],'w').write((d.get('title','')+' '+b)[:600])
PY
  ngt=$(grep -c . "$W/gt-$pr.txt" || echo 0)
  [ "$ngt" -eq 0 ] && { echo -e "$pr\t0\t-\t-\t-\tno_code_change" >> "$TSV"; echo "$pr: no code change"; continue; }
  wt="$W/wt-$pr"; idx="$W/idx-$pr"; rm -rf "$wt" "$idx"
  git -C "$GS" worktree add --detach "$wt" "$sha" >/dev/null 2>&1 || { git -C "$GS" worktree prune; git -C "$GS" worktree add --detach "$wt" "$sha" >/dev/null 2>&1; } || { echo "$pr: worktree fail"; continue; }
  "$CKV" build --src="$wt" --files-from="$FILTER" --out="$idx" --lang=go,solidity --embedder ollama --model-name "$MODEL" >/dev/null 2>&1
  [ -f "$idx/manifest.json" ] || { echo "$pr: build fail"; git -C "$GS" worktree remove --force "$wt" 2>/dev/null; rm -rf "$wt" "$idx"; continue; }
  "$CKV" query "$(cat "$W/q-$pr.txt")" --out "$idx" --embedder ollama --model-name "$MODEL" -k 20 --threshold -1 --json --no-footprint 2>/dev/null \
    | python3 -c "import json,sys;d=json.load(sys.stdin);seen=[]
[seen.append(f) for f in (h.get('citation',{}).get('file','') for h in d.get('hits',[])) if f and f not in seen]
open('$W/ranked-$pr.txt','w').write('\n'.join(seen))"
  res=$(python3 - "$W/gt-$pr.txt" "$W/ranked-$pr.txt" <<'PY'
import sys
gt=[l.strip() for l in open(sys.argv[1]) if l.strip()]
ranked=[l.strip() for l in open(sys.argv[2]) if l.strip()]
rank={f:i+1 for i,f in enumerate(ranked)}
present=[rank[g] for g in gt if g in rank]; n=len(gt)
h5=sum(1 for g in gt if 0<rank.get(g,0)<=5); h10=sum(1 for g in gt if 0<rank.get(g,0)<=10)
mrr=1.0/min(present) if present else 0.0
print(f"{n}\t{h5/n:.3f}\t{h10/n:.3f}\t{mrr:.3f}\t"+','.join(f"{g.split('/')[-1]}:{rank.get(g,0) or 'MISS'}" for g in gt))
PY
)
  echo "$pr: $res"; echo -e "$pr\t$res" >> "$TSV"
  git -C "$GS" worktree remove --force "$wt" 2>/dev/null; rm -rf "$wt" "$idx"
done < "$W/pr_bases.txt"

say "aggregate"
python3 - "$TSV" <<'PY'
import sys,statistics as st
rows=[l.rstrip().split('\t') for l in open(sys.argv[1])][1:]
v=[(float(r[2]),float(r[3]),float(r[4])) for r in rows if len(r)>4 and r[2] not in ('-','')]
if v:
    print(f"PRs: {len(v)}  recall@5={st.mean(x[0] for x in v):.3f}  recall@10={st.mean(x[1] for x in v):.3f}  MRR={st.mean(x[2] for x in v):.3f}")
PY
echo "results: $TSV"
