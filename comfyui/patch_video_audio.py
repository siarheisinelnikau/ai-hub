"""ai-hub patch: sanitize NaN/Inf in the audio waveform before AAC/Opus encode.

LTX-2.5 / MiniMax AudioVAE can emit non-finite samples, which makes PyAV's AAC
encoder abort with:  av.error.ArgumentError: [aac] Input contains (near) NaN/+-Inf
(that kills the whole SaveVideo step even though the video decoded fine).

Fix: replace non-finite samples with 0 right before the frame is encoded.
Idempotent: no-op if already applied or if the waveform is already clean.
"""
import sys

F = "/home/comfy/ComfyUI/comfy_api/latest/_input_impl/video_types.py"
src = open(F).read()

if "nan_to_num" in src:
    print("patch already present, nothing to do")
    sys.exit(0)

old = "                frame = av.AudioFrame.from_ndarray(waveform.float().cpu().contiguous().numpy(), format='fltp', layout=layout)"

san = ("                # ai-hub: sanitize NaN/Inf in audio before AAC/Opus encode\n"
       "                waveform = waveform.nan_to_num(nan=0.0, posinf=0.0, neginf=0.0)\n")

if src.count(old) != 1:
    print("ERROR: expected 1 match, found", src.count(old), file=sys.stderr)
    sys.exit(1)

open(F, "w").write(src.replace(old, san + old))
print("patched video_types.py (audio NaN sanitize)")
