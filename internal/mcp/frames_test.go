package mcp

import (
 "bytes"
 "encoding/base64"
 "encoding/json"
 "image"
 "image/jpeg"
 "testing"
 "thirdcoast.systems/rewind/internal/frames"
)

func TestFrameImagesReachMCPWire(t *testing.T){
 var b bytes.Buffer
 if err:=jpeg.Encode(&b,image.NewRGBA(image.Rect(0,0,20,10)),nil);err!=nil{t.Fatal(err)}
 result,_,err:=frameResult([]frames.Frame{{JPEG:b.Bytes(),Requested:0,Source:"video_frame"},{Error:"media_unavailable"}},nil);if err!=nil{t.Fatal(err)}
 raw,err:=json.Marshal(result);if err!=nil{t.Fatal(err)}
 var wire struct{Content []struct{Type,Text,Data,MimeType string}}
 if err=json.Unmarshal(raw,&wire);err!=nil{t.Fatal(err)}
 if len(wire.Content)!=2||wire.Content[1].Type!="image"||wire.Content[1].MimeType!="image/jpeg"{t.Fatalf("missing image content: %s",raw)}
 decoded,err:=base64.StdEncoding.DecodeString(wire.Content[1].Data);if err!=nil||!bytes.Equal(decoded,b.Bytes()){t.Fatal("JPEG was double encoded or corrupted")}
 var manifest struct{Frames []frames.Frame;Returned int};if err=json.Unmarshal([]byte(wire.Content[0].Text),&manifest);err!=nil{t.Fatal(err)}
 if manifest.Returned!=1||manifest.Frames[0].ContentIndex!=1||manifest.Frames[1].Error==""{t.Fatal("missing content mapping or error")}
}
