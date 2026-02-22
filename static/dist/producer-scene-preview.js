(()=>{function r(R,A,P,C){let E=Number(R);return Number.isFinite(E)?Math.max(A,Math.min(P,E)):C}function O(R){if(typeof R!="string")return null;let A=R.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/);if(!A)return null;let P=r(A[1],.001,1e3,0),C=r(A[2],.001,1e3,0),E=P/C;return!Number.isFinite(E)||E<=0?null:E}(function(){let R="producer-scene-preview-canvas",A="producer-current-scene",P="producer-preview-stage",C="producer-video-frame-rect",E="producer-video-frame-handle",nt="producer-video-frame-readout",F="16:9";function ot(){return document.getElementById(R)}function h(t){return document.getElementById(t)}function rt(){return h(P)}function it(){return h(A)}let b={x:.5,y:.5,width:.9,height:.9},D="",L=Date.now();function st(){return O(F)||16/9}function U(){let t=rt();if(!t)return;let e=typeof F=="string"?F.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/):null;if(!e){t.style.aspectRatio="16 / 9";return}let o=r(e[1],.001,1e3,16),n=r(e[2],.001,1e3,9);t.style.aspectRatio=`${o} / ${n}`}function ct(t,e,o){let c=st(),u=t*c/o,d=e*o/c,a=t,f=u;if(Number.isFinite(u)&&Number.isFinite(d)){let M=Math.abs(u-e);Math.abs(d-t)<M&&(a=d,f=e)}if(!Number.isFinite(a)||!Number.isFinite(f)||a<=0||f<=0)return{width:t,height:e};let x=Math.min(1,1/a,1/f);a*=x,f*=x;let g=Math.max(1,.1/a,.1/f);a*=g,f*=g;let S=Math.min(1,1/a,1/f);return a*=S,f*=S,{width:a,height:f}}function at(t){let e=r(t.width,.1,1,.9),o=r(t.height,.1,1,.9),n=O(D);if(n){let u=ct(e,o,n);e=u.width,o=u.height}let i=r(t.x,e/2,1-e/2,.5),c=r(t.y,o/2,1-o/2,.5);return{x:i,y:c,width:e,height:o}}function I(t){b=at({x:typeof t.x<"u"?t.x:b.x,y:typeof t.y<"u"?t.y:b.y,width:typeof t.width<"u"?t.width:b.width,height:typeof t.height<"u"?t.height:b.height})}function dt(t){let e=h(nt);if(!e)return;let o=(t.video.x*100).toFixed(1),n=(t.video.y*100).toFixed(1),i=(t.video.width*100).toFixed(1),c=(t.video.height*100).toFixed(1),u=t.video.height>0?t.video.width/t.video.height:0,d=Number.isFinite(u)&&u>0?u.toFixed(3):"\u2014",a=typeof t.video.aspect=="string"&&t.video.aspect.trim()!==""?t.video.aspect.trim():"";e.textContent=a?`X ${o}%  Y ${n}%  W ${i}%  H ${c}%  FIX ${a}  AR ${d}`:`X ${o}%  Y ${n}%  W ${i}%  H ${c}%  AR ${d}`}let Y=null;function lt(t){if(typeof t!="string")return null;let e=t.match(/rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)/i);if(e){let n=r(e[1],0,255,255)/255,i=r(e[2],0,255,255)/255,c=r(e[3],0,255,255)/255;return[n,i,c]}let o=t.match(/color\(\s*srgb\s+([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)/i);if(o){let n=r(o[1],0,1,1),i=r(o[2],0,1,1),c=r(o[3],0,1,1);return[n,i,c]}return null}function ut(){if(Y)return Y;if(!document.body)return null;let t=document.createElement("span");return t.style.position="absolute",t.style.left="-9999px",t.style.top="-9999px",t.style.visibility="hidden",document.body.appendChild(t),Y=t,t}function pt(t,e,o){try{if(!window.CSS||typeof CSS.supports!="function"||!CSS.supports("color","oklch(50% 0.1 120)"))return null;let n=ut();if(!n)return null;let i=r(t,0,1,1)*100,c=r(e,0,1,0),u=r(o,0,360,0);return n.style.color=`oklch(${i}% ${c} ${u})`,lt(getComputedStyle(n).color)}catch{return null}}function ft(t,e,o){let n=r(t,0,1,1),i=r(e,0,1,0),c=r(o,0,360,0)*Math.PI/180,u=i*Math.cos(c),d=i*Math.sin(c),a=n+.3963377774*u+.2158037573*d,f=n-.1055613458*u-.0638541728*d,x=n-.0894841775*u-1.291485548*d,g=a*a*a,S=f*f*f,M=x*x*x,T=4.0767416621*g-3.3077115913*S+.2309699292*M,B=-1.2684380046*g+2.6097574011*S-.3413193965*M,w=-.0041960863*g-.7034186147*S+1.707614701*M,z=_=>(_=Math.max(0,Math.min(1,_)),_<=.0031308?12.92*_:1.055*Math.pow(_,1/2.4)-.055);return[z(T),z(B),z(w)]}function ht(t,e,o){return pt(t,e,o)||ft(t,e,o)}function mt(){let t=(h("scene-background-mode")?.value||"perlin-nebula").trim(),e=r(h("scene-speed")?.value,0,10,1),o=r(h("scene-seed")?.value,-1e6,1e6,0),n=!!h("scene-video-border-enabled")?.checked,i=r(h("scene-video-border-size")?.value,0,50,2),c=r(h("scene-video-border-opacity")?.value,0,1,.1),u=r(h("scene-oklch-l")?.value,0,1,1),d=r(h("scene-oklch-c")?.value,0,1,0),a=r(h("scene-oklch-h")?.value,0,360,0);return{stage:{aspect:F},mode:t,speed:e,seed:o,oklch:{l:u,c:d,h:a},video:{x:b.x,y:b.y,width:b.width,height:b.height,aspect:D,border:{enabled:n,size:i,opacity:c}}}}function W(t){if(!t||typeof t!="string")return null;try{return JSON.parse(atob(t))}catch{return null}}function k(t,e){let o=h(t);o&&(o.value=String(e),o.dispatchEvent(new Event("input",{bubbles:!0})),o.dispatchEvent(new Event("change",{bubbles:!0})))}function q(t,e){let o=t&&t.stage?t.stage:null;o&&typeof o.aspect=="string"&&o.aspect.trim()!==""?F=o.aspect.trim():F="16:9",U();let n=t&&t.background?t.background:null,i=n&&n.mode?String(n.mode):"perlin-nebula",c=n&&typeof n.speed<"u"?n.speed:1,u=n&&typeof n.seed<"u"?n.seed:0;if(k("scene-background-mode",i),k("scene-speed",r(c,0,10,1)),k("scene-seed",r(u,-1e6,1e6,0)),n&&n.tint_oklch&&typeof n.tint_oklch=="object"){let f=r(n.tint_oklch.l,0,1,1),x=r(n.tint_oklch.c,0,1,0),g=r(n.tint_oklch.h,0,360,0);k("scene-oklch-l",f),k("scene-oklch-c",x),k("scene-oklch-h",g)}let d=t&&t.video?t.video:null,a=d&&d.border?d.border:null;if(d){D=typeof d.aspect=="string"?d.aspect:"",I({x:r(d.x,0,1,.5),y:r(d.y,0,1,.5),width:r(d.width??d.w,.1,1,.9),height:r(d.height??d.h,.1,1,.9)});let f=h("scene-video-border-enabled");f&&(f.checked=a&&typeof a.enabled<"u"?!!a.enabled:!0),k("scene-video-border-size",r(a&&a.size,0,50,2)),k("scene-video-border-opacity",r(a&&a.opacity,0,1,.1))}typeof e=="string"&&e.trim()!==""&&k("scene-preset-name",e.trim())}function vt(t){let e=h(C);if(!e)return;e.style.left=(t.video.x*100).toFixed(3)+"%",e.style.top=(t.video.y*100).toFixed(3)+"%",e.style.width=(t.video.width*100).toFixed(3)+"%",e.style.height=(t.video.height*100).toFixed(3)+"%",e.style.transform="translate(-50%, -50%)",dt(t);let o=t.video.border;o&&o.enabled&&o.size>0&&o.opacity>0?(e.style.borderStyle="solid",e.style.borderWidth=Math.round(o.size)+"px",e.style.borderColor=`rgba(255,255,255,${o.opacity})`):e.style.borderWidth="0px"}function bt(t){let e=(o,n)=>{let i=h(o);i&&(i.value=String(n))};e("scene-apply-background-mode",t.mode),e("scene-apply-stage-aspect",t.stage&&typeof t.stage.aspect=="string"?t.stage.aspect:F),e("scene-apply-speed",t.speed),e("scene-apply-seed",t.seed),e("scene-apply-oklch-l",t.oklch.l),e("scene-apply-oklch-c",t.oklch.c),e("scene-apply-oklch-h",t.oklch.h),e("scene-apply-epoch-ms",L),t.video&&(e("scene-apply-video-x",t.video.x),e("scene-apply-video-y",t.video.y),e("scene-apply-video-w",t.video.width),e("scene-apply-video-h",t.video.height),e("scene-apply-video-aspect",t.video.aspect||""),e("scene-apply-video-border-enabled",t.video.border&&t.video.border.enabled?1:0),e("scene-apply-video-border-size",t.video.border?t.video.border.size:0),e("scene-apply-video-border-opacity",t.video.border?t.video.border.opacity:0),e("scene-save-stage-aspect",t.stage&&typeof t.stage.aspect=="string"?t.stage.aspect:F),e("scene-save-video-x",t.video.x),e("scene-save-video-y",t.video.y),e("scene-save-video-w",t.video.width),e("scene-save-video-h",t.video.height),e("scene-save-video-aspect",t.video.aspect||""))}function j(t,e,o){let n=t.createShader(e);return!n||(t.shaderSource(n,o),t.compileShader(n),!t.getShaderParameter(n,t.COMPILE_STATUS))?null:n}function gt(t,e,o){let n=j(t,t.VERTEX_SHADER,e),i=j(t,t.FRAGMENT_SHADER,o);if(!n||!i)return null;let c=t.createProgram();return!c||(t.attachShader(c,n),t.attachShader(c,i),t.linkProgram(c),!t.getProgramParameter(c,t.LINK_STATUS))?null:c}let yt=`
    attribute vec2 a_position;
    varying vec2 v_uv;
    void main() {
      v_uv = a_position * 0.5 + 0.5;
      gl_Position = vec4(a_position, 0.0, 1.0);
    }
  `,xt=`
    precision highp float;

    varying vec2 v_uv;
    uniform vec2 u_resolution;
    uniform float u_time;
    uniform float u_seed;
    uniform vec3 u_tint;

    float hash12(vec2 p) {
      vec3 p3 = fract(vec3(p.xyx) * 0.1031);
      p3 += dot(p3, p3.yzx + 33.33);
      return fract((p3.x + p3.y) * p3.z);
    }

    vec2 hash22(vec2 p) {
      float n = hash12(p);
      float m = hash12(p + 19.19);
      return vec2(n, m) * 2.0 - 1.0;
    }

    float gradNoise(vec2 p) {
      vec2 i = floor(p);
      vec2 f = fract(p);
      vec2 u = f * f * f * (f * (f * 6.0 - 15.0) + 10.0);

      vec2 g00 = normalize(hash22(i + vec2(0.0, 0.0)));
      vec2 g10 = normalize(hash22(i + vec2(1.0, 0.0)));
      vec2 g01 = normalize(hash22(i + vec2(0.0, 1.0)));
      vec2 g11 = normalize(hash22(i + vec2(1.0, 1.0)));

      float n00 = dot(g00, f - vec2(0.0, 0.0));
      float n10 = dot(g10, f - vec2(1.0, 0.0));
      float n01 = dot(g01, f - vec2(0.0, 1.0));
      float n11 = dot(g11, f - vec2(1.0, 1.0));

      float nx0 = mix(n00, n10, u.x);
      float nx1 = mix(n01, n11, u.x);
      return mix(nx0, nx1, u.y);
    }

    float fbm(vec2 p) {
      float sum = 0.0;
      float amp = 0.55;
      float freq = 1.0;
      for (int i = 0; i < 6; i++) {
        sum += amp * gradNoise(p * freq);
        freq *= 2.0;
        amp *= 0.5;
      }
      return sum;
    }

    void main() {
      vec2 uv = v_uv;

      float aspect = u_resolution.x / max(1.0, u_resolution.y);
      vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
      p += vec2(u_seed * 0.013, u_seed * 0.021);

      float t = u_time * 0.06;
      vec2 drift = vec2(0.18 * t, -0.11 * t);

      float w1 = fbm(p * 2.3 + drift);
      float w2 = fbm(p * 3.7 - drift * 1.3);
      vec2 warp = vec2(w1, w2) * 0.55;

      float n = fbm(p * 3.0 + warp + drift);

      n = 0.5 + 0.5 * n;
      n = smoothstep(0.15, 0.95, n);

      float r = length(p);
      float vignette = smoothstep(1.1, 0.25, r);

      float intensity = (n * 0.75 * 0.8) * vignette;

      vec3 col = u_tint * intensity;
      gl_FragColor = vec4(col, 1.0);
    }
  `;function J(){let t=ot();if(!t)return;let e=h(C),o=h(E),n=t.getContext("webgl",{alpha:!1,antialias:!1,depth:!1,stencil:!1,premultipliedAlpha:!1,preserveDrawingBuffer:!1,powerPreference:"high-performance"});if(!n)return;let i=gt(n,yt,xt);if(!i)return;let c=n.getAttribLocation(i,"a_position"),u=n.getUniformLocation(i,"u_resolution"),d=n.getUniformLocation(i,"u_time"),a=n.getUniformLocation(i,"u_seed"),f=n.getUniformLocation(i,"u_tint"),x=n.createBuffer();n.bindBuffer(n.ARRAY_BUFFER,x),n.bufferData(n.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),n.STATIC_DRAW);function g(){let s=Math.min(2,window.devicePixelRatio||1),l=Math.max(1,Math.floor(t.clientWidth*s)),m=Math.max(1,Math.floor(t.clientHeight*s));(t.width!==l||t.height!==m)&&(t.width=l,t.height=m,n.viewport(0,0,l,m))}L=Date.now();let S=h("scene-apply-form"),M=0,T=!1,B=!1,w=()=>{S&&(M&&window.clearTimeout(M),M=window.setTimeout(()=>{M=0,z()},150))},z=async()=>{if(S){if(T){B=!0;return}T=!0,B=!1;try{let s=new FormData(S);await fetch(S.action,{method:"POST",body:s,credentials:"same-origin",redirect:"manual"})}catch{}finally{T=!1,B&&w()}}};U();let _=it();if(_&&_.dataset&&_.dataset.sceneB64){let s=W(_.dataset.sceneB64);s&&(q(s,""),L=Date.now())}function St(){let s=mt(),l=ht(s.oklch.l,s.oklch.c,s.oklch.h);return bt(s),vt(s),t.style.display=s.mode==="none"?"none":"",{cfg:s,rgb:l}}let N=0;function Q(){if(N=requestAnimationFrame(Q),document.visibilityState==="hidden")return;g();let{cfg:s,rgb:l}=St();s.mode!=="none"&&(n.useProgram(i),n.enableVertexAttribArray(c),n.bindBuffer(n.ARRAY_BUFFER,x),n.vertexAttribPointer(c,2,n.FLOAT,!1,0,0),n.uniform2f(u,t.width,t.height),n.uniform1f(d,(Date.now()-L)/1e3*s.speed),n.uniform1f(a,s.seed),n.uniform3f(f,l[0],l[1],l[2]),n.drawArrays(n.TRIANGLE_STRIP,0,4))}if(t.addEventListener("click",()=>{L=Date.now(),w()},{passive:!0}),document.addEventListener("click",s=>{let l=s.target;if(!(l instanceof Element))return;let m=l.closest("button[data-scene-b64]");if(!m)return;let v=W(m.getAttribute("data-scene-b64")),$=m.getAttribute("data-preset-name")||"";v&&(q(v,$),L=Date.now(),w())}),document.addEventListener("click",s=>{let l=s.target;if(!(l instanceof Element))return;let m=l.closest("button[data-stage-aspect]");if(!m)return;let v=(m.getAttribute("data-stage-aspect")||"").trim();O(v)&&(F=v,U(),O(D)&&I({width:b.width,height:b.height}),w(),s.preventDefault())}),document.addEventListener("click",s=>{let l=s.target;if(!(l instanceof Element))return;let m=l.closest("button[data-video-aspect]");if(!m)return;let v=m.getAttribute("data-video-aspect")||"",$=v.match(/^\s*(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)\s*$/);if(!$)return;let G=r($[1],.001,1e3,16),X=r($[2],.001,1e3,9),H=G/X;!Number.isFinite(H)||H<=0||(D=v,I({width:b.width,height:b.height}),w(),s.preventDefault())}),e){let s=()=>{let p=e.parentElement;return p?p.getBoundingClientRect():null},l=(p,y)=>{try{p.setPointerCapture(y.pointerId)}catch{}},m=null,v=null,$=p=>{if(!(p instanceof PointerEvent)||o&&(p.target===o||p.target instanceof Element&&p.target.closest("#"+E)))return;let y=s();y&&(m="move",v={x:p.clientX,y:p.clientY,state:{...b},rect:y},l(e,p),p.preventDefault())},G=p=>{if(!(p instanceof PointerEvent))return;let y=s();y&&(m="resize",v={x:p.clientX,y:p.clientY,state:{...b},rect:y},l(o||e,p),p.preventDefault())},X=p=>{if(!m||!v)return;let y=v.rect,Z=(p.clientX-v.x)/Math.max(1,y.width),tt=(p.clientY-v.y)/Math.max(1,y.height);if(m==="move"){let K=v.state.x+Z,V=v.state.y+tt,et=10,Mt=et/Math.max(1,y.width),_t=et/Math.max(1,y.height);Math.abs(K-.5)<=Mt&&(K=.5),Math.abs(V-.5)<=_t&&(V=.5),I({x:K,y:V}),w()}else m==="resize"&&(I({width:v.state.width+Z*2,height:v.state.height+tt*2}),w())},H=()=>{m=null,v=null};e.addEventListener("pointerdown",$),e.addEventListener("pointermove",X),e.addEventListener("pointerup",H),e.addEventListener("pointercancel",H),o&&o.addEventListener("pointerdown",G)}let wt=["scene-background-mode","scene-speed","scene-seed","scene-oklch-l","scene-oklch-c","scene-oklch-h","scene-video-border-size","scene-video-border-opacity","scene-video-border-enabled"];for(let s of wt){let l=h(s);l&&(l.addEventListener("input",()=>{w()}),l.addEventListener("change",()=>{w()}))}window.addEventListener("resize",g,{passive:!0}),g(),N=requestAnimationFrame(Q),window.__rewindProducerScenePreview={resetEpoch:()=>{L=Date.now()},stop:()=>{N&&cancelAnimationFrame(N),N=0}}}document.readyState==="loading"?document.addEventListener("DOMContentLoaded",J):J()})();})();
