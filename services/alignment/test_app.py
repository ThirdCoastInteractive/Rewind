import unittest,tempfile,sys
from pathlib import Path
import app
class Tests(unittest.TestCase):
 def test_missing(self):self.assertEqual(app.align_payload({'audio_path':'/missing','text':'x','language':'en'})['state'],'unalignable')
 def test_missing_model(self):
  with tempfile.TemporaryDirectory() as d:
   p=Path(d)/'a.wav';p.write_bytes(b'x')
   class W:
    def load_align_model(self,**kw):raise FileNotFoundError('model')
   old=sys.modules.get('whisperx');sys.modules['whisperx']=W()
   try:self.assertEqual(app.align_payload({'audio_path':str(p),'text':'x','language':'en','end':1})['state'],'missing_model')
   finally:
    if old is None:del sys.modules['whisperx']
    else:sys.modules['whisperx']=old
 def test_missing_word_is_partial(self):
  with tempfile.TemporaryDirectory() as d:
   p=Path(d)/'a.wav';p.write_bytes(b'x')
   class W:
    def load_align_model(self,**kw):return object(),{}
    def load_audio(self,*a):return object()
    def align(self,*a,**kw):return {'segments':[{'words':[{'word':'hello','start':0.1,'end':0.3}]}]}
   old=sys.modules.get('whisperx');sys.modules['whisperx']=W()
   try:self.assertEqual(app.align_payload({'audio_path':str(p),'text':'hello world','language':'en','end':1})['state'],'partial_unalignable')
   finally:
    if old is None:del sys.modules['whisperx']
    else:sys.modules['whisperx']=old
if __name__=='__main__':unittest.main()
