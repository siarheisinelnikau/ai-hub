"""ai-hub custom node: Qwen-Image 2.1 prompt-rewriter (PE) helper.

The Qwen-Image 2.1 PE models (``qwen3.5_9b_qwen_image_2.1_pe_t2i/i2i``) are used with the
core ``TextGenerate`` node and answer with a ``<think>...</think>`` block followed by JSON:

    {"rewritten_prompt": "...", "wh_ratio": "16:9"}
    {"rewritten_prompt": "...", "wh_ratio": "", "ratio_follow": "<image1>"}   # PE-I2I in-place edit

This node turns that raw text into the pieces a graph needs: the rewritten prompt, an
aspect-ratio derived width/height (snapped to ``multiple``), an area-equivalent
``resolution`` for ``TextEncodeQwenImage21`` and the ratio string itself.

No external dependencies; parsing is string-aware so braces inside the prompt text are safe.
"""

from __future__ import annotations

import json
import math
import re

from typing_extensions import override

from comfy_api.latest import ComfyExtension, io

BASE_PIXELS = 1024 * 1024  # 1.0 "megapixel" = 1024x1024, same convention as ResolutionSelector
RATIO_RE = re.compile(r"(\d+(?:\.\d+)?)\s*[:x\u00d7/]\s*(\d+(?:\.\d+)?)")
THINK_END_RE = re.compile(r"</think\w*>", re.IGNORECASE)


def _balanced_objects(text: str) -> list[str]:
    """Return every balanced ``{...}`` substring, string-literal aware."""
    objects: list[str] = []
    depth = 0
    start = None
    in_str = False
    esc = False
    for i, ch in enumerate(text):
        if in_str:
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
            continue
        if ch == '"':
            in_str = True
        elif ch == "{":
            if depth == 0:
                start = i
            depth += 1
        elif ch == "}" and depth > 0:
            depth -= 1
            if depth == 0 and start is not None:
                objects.append(text[start : i + 1])
                start = None
    return objects


def _extract_payload(text: str) -> dict | None:
    """Find the last JSON object in the model output that looks like a PE reply."""
    candidates = _balanced_objects(text)
    for obj in reversed(candidates):
        try:
            data = json.loads(obj)
        except (json.JSONDecodeError, TypeError):
            continue
        if not isinstance(data, dict):
            continue
        if "rewritten_prompt" in data:
            return data
        if "prompt" in data:  # tolerated variant
            data.setdefault("wh_ratio", "")
            data["rewritten_prompt"] = data.get("prompt", "")
            return data
    return None


def _prompt_from_text(text: str) -> str:
    """Fallback when no JSON is present: drop a thinking block if one is visible."""
    matches = list(THINK_END_RE.finditer(text))
    if matches:
        return text[matches[-1].end() :].strip()
    return text.strip()


def _fit_ratio(ratio: str, megapixels: float, multiple: int) -> tuple[int, int] | None:
    """Aspect ratio + area budget -> width/height snapped to ``multiple``."""
    m = RATIO_RE.search(ratio or "")
    if not m:
        return None
    a, b = float(m.group(1)), float(m.group(2))
    if a <= 0 or b <= 0:
        return None
    target = max(megapixels, 0.05) * BASE_PIXELS
    width = math.sqrt(target * a / b)
    height = math.sqrt(target * b / a)
    snap = max(8, int(multiple))
    width = max(snap, int(round(width / snap)) * snap)
    height = max(snap, int(round(height / snap)) * snap)
    return min(width, 4096), min(height, 4096)


class Qwen21PEPrompt(io.ComfyNode):
    @classmethod
    def define_schema(cls) -> io.Schema:
        return io.Schema(
            node_id="Qwen21PEPrompt",
            display_name="Qwen 2.1 PE: prompt + size",
            category="aihub/text",
            description=(
                "Parse the raw TextGenerate output of a Qwen-Image 2.1 prompt-rewriter (PE) model into "
                "rewritten_prompt / width / height / resolution. Falls back to fallback_width/height when "
                "the reply carries no usable wh_ratio (PE-I2I in-place edits return ratio_follow instead)."
            ),
            inputs=[
                io.String.Input("text", multiline=True, tooltip="Raw TextGenerate output (thinking + JSON)"),
                io.Float.Input("megapixels", default=1.0, min=0.1, max=8.0, step=0.05,
                               tooltip="Area budget for the output image, 1.0 = 1024x1024"),
                io.Int.Input("multiple", default=32, min=8, max=128, step=8,
                             tooltip="Snap width/height to this multiple"),
                io.Int.Input("fallback_width", default=1024, min=64, max=4096, step=32),
                io.Int.Input("fallback_height", default=1024, min=64, max=4096, step=32),
            ],
            outputs=[
                io.String.Output(display_name="prompt"),
                io.Int.Output(display_name="width"),
                io.Int.Output(display_name="height"),
                io.Int.Output(display_name="resolution"),
                io.String.Output(display_name="ratio"),
            ],
        )

    @classmethod
    def execute(cls, text, megapixels, multiple, fallback_width, fallback_height) -> io.NodeOutput:
        data = _extract_payload(text)
        if data is None:
            prompt = _prompt_from_text(text)
            ratio = ""
        else:
            prompt = str(data.get("rewritten_prompt") or "").strip()
            ratio = str(data.get("wh_ratio") or "").strip()

        fitted = _fit_ratio(ratio, megapixels, multiple)
        if fitted is None:
            width = max(8, int(fallback_width))
            height = max(8, int(fallback_height))
        else:
            width, height = fitted

        resolution = int(round(math.sqrt(width * height)))
        if multiple > 8:
            snap = max(8, int(multiple))
            resolution = max(snap, int(round(resolution / snap)) * snap)
        return io.NodeOutput(prompt, int(width), int(height), int(resolution), ratio)


class AihubQwen21Extension(ComfyExtension):
    @override
    async def get_node_list(self) -> list[type[io.ComfyNode]]:
        return [Qwen21PEPrompt]


async def comfy_entrypoint() -> AihubQwen21Extension:
    return AihubQwen21Extension()
