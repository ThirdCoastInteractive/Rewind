"""WhisperX forced alignment service; it never transcribes audio."""
import json, sys, os, contextlib, re
from pathlib import Path

_models = {}

def align_payload(req, runtime=None):
    """Align supplied transcript text and return explicit result state."""
    audio=Path(req.get("audio_path", "")); text=req.get("text", ""); lang=req.get("language", ""); start=float(req.get("start",0)); end=float(req.get("end",0))
    if not text or not lang or not audio.is_file() or start<0 or end<=start: return {"state":"unalignable","words":[],"error":"missing_input"}
    try:
        import whisperx
        device="cpu"
        key=(lang,req.get("model","default"),req.get("version",""))
        if key not in _models:
            with contextlib.redirect_stdout(sys.stderr):
                model,meta=whisperx.load_align_model(language_code=lang,device=device,model_name=req.get("model"),model_dir=os.getenv("ALIGNMENT_MODEL_DIR","/models/alignment"))
            _models[key]=(model,meta)
        model,meta=_models[key]
        audio_data=whisperx.load_audio(str(audio)); segments=[{"text":text,"start":0.0,"end":end-start}]
        with contextlib.redirect_stdout(sys.stderr):
            result=whisperx.align(segments,model,meta,audio_data,device,return_char_alignments=False)
        words=[]
        missing=False
        for seg in result.get("segments",[]):
            for w in seg.get("words",[]):
                word=w.get("word","");ws=w.get("start");we=w.get("end")
                if not word:continue
                if ws is None or we is None:missing=True;words.append({"text":word,"start":None,"end":None});continue
                if ws < 0 or we <= ws or we > end-start: missing=True
                words.append({"text":word,"start":ws+start,"end":we+start})
        prev = start
        for w in words:
            if w["start"] is None or w["start"] < prev: missing=True
            else: prev = w["end"]
        expected = re.findall(r"[\w']+", text.lower()); returned = re.findall(r"[\w']+", " ".join(w["text"] for w in words).lower())
        if expected != returned: missing=True
        return {"state":"partial_unalignable" if missing else ("valid" if words else "unalignable"),"words":words,"language":lang}
    except ValueError as e: return {"state":"unsupported_language","words":[],"error":str(e)}
    except (ImportError,FileNotFoundError) as e: return {"state":"missing_model","words":[],"error":str(e)}
    except Exception as e: return {"state":"error","words":[],"error":str(e)}

def main():
    for line in sys.stdin:
        try: print(json.dumps(align_payload(json.loads(line))))
        except Exception as e: print(json.dumps({"state":"error","words":[],"error":str(e)}))

if __name__=="__main__": main()
