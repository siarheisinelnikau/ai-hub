# Third-party components and assets

No model weights are distributed. Every model, engine, image, provider and
dataset has its own terms. Integration names do not imply endorsement.

| Component | Use / source |
| --- | --- |
| Go standard library | Backend; [Go license](https://go.dev/LICENSE) |
| gopkg.in/yaml.v3 | YAML; [upstream license](https://github.com/go-yaml/yaml/blob/v3/LICENSE) |
| FastAPI, Uvicorn, psutil, PyYAML | Development-only API compatibility fixture |
| Inter and JetBrains Mono | Optional Google Fonts stylesheet; system-font fallbacks |
| llama.cpp, vLLM, SGLang, Strata, Maya | Separately installed engines; independent licenses |
| ComfyUI and media dependencies | Optional container recipes; dependencies obtained separately |

The SVG family illustrations in `static/img/models/` were authored for ai-hub;
they are not copied provider logos. Screenshots use synthetic data from
`scripts/demo.py` and contain no production telemetry.

Private operational reports, hardware photographs, vendored case viewers and
raw conversations are excluded from the public snapshot. Review original
licenses and notices before redistributing patched engine sources.
