#!/usr/bin/env bash
set -e
COMFYUI_DIR="/home/comfy/ComfyUI"

# Fix permissions on mounted volumes
for DIR in models output input user; do
    TARGET="${COMFYUI_DIR}/${DIR}"
    if [ -d "${TARGET}" ]; then
        chmod -R 777 "${TARGET}" 2>/dev/null || true
    fi
done

echo "Starting ComfyUI..."

# --- Pixal3D template fix: UnwrapMesh UV-pack 4096 -> 1024 (OOM on a 24 GB 3090) ---
# The official template feeds UnwrapMesh.resolution from PrimitiveInt(288)=4096 and the
# UV packing OOMs in _pack_bitmap_torch. Unlink it and pin 1024; 288 stays 4096 so the
# BakeTextureFromVoxel.texture_size keeps the full texture resolution. Idempotent.
TPL="/home/comfy/venv/lib/python3.11/site-packages/comfyui_workflow_templates_json/templates/3d_pixal3d_trellis2_image_to_model.json"
if [ -f "$TPL" ] && [ -x /home/comfy/venv/bin/python ]; then
    /home/comfy/venv/bin/python - "$TPL" <<'PYEOF' || true
import json, sys
p = sys.argv[1]
try:
    d = json.load(open(p))
except Exception as e:
    print(f"template patch skipped: {e}")
    raise SystemExit(0)
changed = False
for n in d.get("nodes", []):
    if n.get("id") == 196 and n.get("type") == "UnwrapMesh":
        if any(i.get("name") == "resolution" and i.get("link") is not None for i in n.get("inputs", [])):
            d["links"] = [l for l in d.get("links", []) if not (l[1] == 288 and l[3] == 196)]
            for i in n.get("inputs", []):
                if i.get("name") == "resolution":
                    i.pop("link", None)
            n["widgets_values"] = ["pec", 1024, 1, 0.0002]
            n["widgets_values_named"] = {"segmenter": "pec", "resolution": 1024, "padding": 1, "weld_distance": 0.0002}
            changed = True
if changed:
    json.dump(d, open(p, "w"), ensure_ascii=False)
    print("patched Pixal3D template: UnwrapMesh.resolution 4096 -> 1024")
PYEOF
fi

cd "${COMFYUI_DIR}"
exec "$@"
