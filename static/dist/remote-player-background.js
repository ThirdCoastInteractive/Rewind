(()=>{var H=window.RewindPage?.scope;function R(m){return H?.cleanup(m)||(()=>{})}function k(m,d,v,y){m.addEventListener(d,v,y),R(()=>m.removeEventListener(d,v,y))}var T=class extends MutationObserver{constructor(d){super(d),R(()=>this.disconnect())}};function $(m){if(H?.signal.aborted)return 0;let d=requestAnimationFrame(y=>{v(),m(y)}),v=R(()=>cancelAnimationFrame(d));return d}function s(m,d,v,y){let f=Number(m);return Number.isFinite(f)?Math.max(d,Math.min(v,f)):y}function N(m){if(typeof m!="string")return null;let d=m.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/);if(!d)return null;let v=s(d[1],.001,1e3,0),y=s(d[2],.001,1e3,0),f=v/y;return!Number.isFinite(f)||f<=0?null:f}(function(){let m="remote-player-bg-canvas",d="player-scene",v="remote-player-video-frame",y=1.7777777777777777,f={speed:1,seed:0,tint:[1,1,1]};function I(){return document.getElementById(m)}function D(){return document.getElementById(d)}function G(){return document.getElementById(v)}function E(){let e=D();if(!e||!e.dataset)return null;let t=e.dataset.sceneB64;if(!t)return null;try{let n=atob(t);return JSON.parse(n)}catch{return null}}function U(e){if(typeof e!="string")return null;let t=e.trim();if(t.startsWith("#")&&(t=t.slice(1)),t.length===3&&(t=t.split("").map(i=>i+i).join("")),!/^[0-9a-fA-F]{6}$/.test(t))return null;let n=parseInt(t.slice(0,2),16)/255,r=parseInt(t.slice(2,4),16)/255,o=parseInt(t.slice(4,6),16)/255;return[n,r,o]}function V(e){let t=U(e);if(t)return t;if(Array.isArray(e)&&e.length>=3){let n=Number(e[0]),r=Number(e[1]),o=Number(e[2]);if(![n,r,o].every(Number.isFinite))return null;let i=n>1||r>1||o>1?255:1;return[Math.max(0,Math.min(1,n/i)),Math.max(0,Math.min(1,r/i)),Math.max(0,Math.min(1,o/i))]}return null}let C=null;function K(e){if(typeof e!="string")return null;let t=e.match(/rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)/i);if(t){let r=s(t[1],0,255,255)/255,o=s(t[2],0,255,255)/255,i=s(t[3],0,255,255)/255;return[r,o,i]}let n=e.match(/color\(\s*srgb\s+([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)/i);if(n){let r=s(n[1],0,1,1),o=s(n[2],0,1,1),i=s(n[3],0,1,1);return[r,o,i]}return null}function W(){if(C)return C;if(!document.body)return null;let e=document.createElement("span");return e.style.position="absolute",e.style.left="-9999px",e.style.top="-9999px",e.style.visibility="hidden",document.body.appendChild(e),C=e,e}function q(e,t,n){try{if(!window.CSS||typeof CSS.supports!="function"||!CSS.supports("color","oklch(50% 0.1 120)"))return null;let r=W();if(!r)return null;let o=s(e,0,1,1)*100,i=s(t,0,1,0),u=s(n,0,360,0);return r.style.color=`oklch(${o}% ${i} ${u})`,K(getComputedStyle(r).color)}catch{return null}}function j(e,t,n){let r=s(e,0,1,1),o=s(t,0,1,0),i=s(n,0,360,0)*Math.PI/180,u=o*Math.cos(i),l=o*Math.sin(i),c=r+.3963377774*u+.2158037573*l,a=r-.1055613458*u-.0638541728*l,b=r-.0894841775*u-1.291485548*l,h=c*c*c,x=a*a*a,p=b*b*b,w=4.0767416621*h-3.3077115913*x+.2309699292*p,g=-1.2684380046*h+2.6097574011*x-.3413193965*p,_=-.0041960863*h-.7034186147*x+1.707614701*p,F=A=>(A=Math.max(0,Math.min(1,A)),A<=.0031308?12.92*A:1.055*Math.pow(A,1/2.4)-.055);return[F(w),F(g),F(_)]}function Y(e,t,n){return q(e,t,n)||j(e,t,n)}function P(e){let t=e&&e.background?e.background:null,n=s(t&&t.speed,0,10,f.speed),r=s(t&&t.seed,-1e6,1e6,f.seed),o=null;if(t&&t.tint_oklch&&typeof t.tint_oklch=="object"){let u=t.tint_oklch.l,l=t.tint_oklch.c,c=t.tint_oklch.h;o=Y(u,l,c)}o||(o=V(t&&t.tint));let i=s(t&&t.epoch_ms,0,9e15,0);return{speed:n,seed:r,tint:o||f.tint,epochMs:i}}let S={x:.5,y:.5,width:.9,height:.9,aspect:"",border:{enabled:!0,size:2,opacity:.1}};function J(e){let t=e&&e.stage?e.stage:null;return N(t&&t.aspect)||y}function X(e,t,n,r){let u=e*r/n,l=t*n/r,c=e,a=u;if(Number.isFinite(u)&&Number.isFinite(l)){let p=Math.abs(u-t);Math.abs(l-e)<p&&(c=l,a=t)}if(!Number.isFinite(c)||!Number.isFinite(a)||c<=0||a<=0)return{width:e,height:t};let b=Math.min(1,1/c,1/a);c*=b,a*=b;let h=Math.max(1,.1/c,.1/a);c*=h,a*=h;let x=Math.min(1,1/c,1/a);return c*=x,a*=x,{width:c,height:a}}function Q(e){let t=Math.max(1,window.innerWidth||1),n=Math.max(1,window.innerHeight||1);if(t/n>=e){let l=n,c=l*e;return{left:(t-c)/2,top:0,width:c,height:l}}let o=t,i=o/e;return{left:0,top:(n-i)/2,width:o,height:i}}function Z(e){let t=e&&e.video?e.video:null,n=t&&t.border?t.border:null;return{x:s(t&&t.x,0,1,S.x),y:s(t&&t.y,0,1,S.y),width:s(t&&(t.width??t.w),.05,1,S.width),height:s(t&&(t.height??t.h),.05,1,S.height),aspect:typeof(t&&t.aspect)=="string"?t.aspect:S.aspect,border:{enabled:!!(n&&typeof n.enabled<"u"?n.enabled:S.border.enabled),size:s(n&&n.size,0,50,S.border.size),opacity:s(n&&n.opacity,0,1,S.border.opacity)}}}function tt(e){let t=G();if(!t)return;let n=Z(e),r=J(e),o=Q(r),i=n.width,u=n.height,l=N(n.aspect);if(l){let x=X(i,u,l,r);i=x.width,u=x.height}let c=o.left+n.x*o.width,a=o.top+n.y*o.height,b=i*o.width,h=u*o.height;t.style.left=`${c.toFixed(1)}px`,t.style.top=`${a.toFixed(1)}px`,t.style.width=`${b.toFixed(1)}px`,t.style.height=`${h.toFixed(1)}px`,t.style.transform="translate(-50%, -50%)",n.border.enabled&&n.border.size>0&&n.border.opacity>0?(t.style.borderStyle="solid",t.style.borderWidth=Math.round(n.border.size)+"px",t.style.borderColor=`rgba(255,255,255,${n.border.opacity})`):t.style.borderWidth="0px"}function B(e,t,n){let r=e.createShader(t);return!r||(e.shaderSource(r,n),e.compileShader(r),!e.getShaderParameter(r,e.COMPILE_STATUS))?null:r}function et(e,t,n){let r=B(e,e.VERTEX_SHADER,t),o=B(e,e.FRAGMENT_SHADER,n);if(!r||!o)return null;let i=e.createProgram();return!i||(e.attachShader(i,r),e.attachShader(i,o),e.linkProgram(i),!e.getProgramParameter(i,e.LINK_STATUS))?null:i}let nt=`
    attribute vec2 a_position;
    varying vec2 v_uv;
    void main() {
      v_uv = a_position * 0.5 + 0.5;
      gl_Position = vec4(a_position, 0.0, 1.0);
    }
  `,rt=`
    precision highp float;

    varying vec2 v_uv;
    uniform vec2 u_resolution;
    uniform float u_time;
    uniform float u_seed;
    uniform vec3 u_tint;

    // Hash-based gradient noise (2D)
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

      // Quintic fade
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

      // Normalize to preserve aspect
      float aspect = u_resolution.x / max(1.0, u_resolution.y);
      vec2 p = (uv - 0.5) * vec2(aspect, 1.0);

      // Seed sync: shift the domain deterministically.
      // Keep the scale small so large seeds stay well-behaved.
      p += vec2(u_seed * 0.013, u_seed * 0.021);

      // Subtle, slow animation
      float t = u_time * 0.06;
      vec2 drift = vec2(0.18 * t, -0.11 * t);

      // Domain warp for a more "nebula" feel
      float w1 = fbm(p * 2.3 + drift);
      float w2 = fbm(p * 3.7 - drift * 1.3);
      vec2 warp = vec2(w1, w2) * 0.55;

      float n = fbm(p * 3.0 + warp + drift);

      // Contrast curve
      n = 0.5 + 0.5 * n;
      n = smoothstep(0.15, 0.95, n);

      // Vignette
      float r = length(p);
      float vignette = smoothstep(1.1, 0.25, r);

      float intensity = (n * 0.75 * 0.8) * vignette;

      // Output tint on black.
      vec3 col = u_tint * intensity;
      gl_FragColor = vec4(col, 1.0);
    }
  `;function ot(e){let t=I();if(!t)return null;let n=t.getContext("webgl",{alpha:!1,antialias:!1,depth:!1,stencil:!1,premultipliedAlpha:!1,preserveDrawingBuffer:!1,powerPreference:"high-performance"});if(!n)return null;let r=et(n,nt,rt);if(!r)return null;let o=n.getAttribLocation(r,"a_position"),i=n.getUniformLocation(r,"u_resolution"),u=n.getUniformLocation(r,"u_time"),l=n.getUniformLocation(r,"u_seed"),c=n.getUniformLocation(r,"u_tint"),a=n.createBuffer();n.bindBuffer(n.ARRAY_BUFFER,a),n.bufferData(n.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),n.STATIC_DRAW);function b(){let g=Math.min(2,window.devicePixelRatio||1),_=Math.max(1,Math.floor(t.clientWidth*g)),F=Math.max(1,Math.floor(t.clientHeight*g));(t.width!==_||t.height!==F)&&(t.width=_,t.height=F,n.viewport(0,0,_,F))}let h=0,x=Date.now(),p={speed:e&&e.speed||f.speed,seed:e&&e.seed||f.seed,tint:e&&e.tint||f.tint,epochMs:e&&e.epochMs||0};function w(g){if(h=$(w),document.visibilityState==="hidden")return;b(),n.useProgram(r),n.enableVertexAttribArray(o),n.bindBuffer(n.ARRAY_BUFFER,a),n.vertexAttribPointer(o,2,n.FLOAT,!1,0,0),n.uniform2f(i,t.width,t.height);let _=p.epochMs&&p.epochMs>0?p.epochMs:x;n.uniform1f(u,(Date.now()-_)/1e3*(p.speed||0)),n.uniform1f(l,p.seed||0),n.uniform3f(c,p.tint[0],p.tint[1],p.tint[2]),n.drawArrays(n.TRIANGLE_STRIP,0,4)}return k(window,"resize",b,{passive:!0}),k(document,"visibilitychange",()=>{document.visibilityState==="visible"&&(x=Date.now())}),b(),h=$(w),{setConfig:g=>{g&&(p={speed:s(g.speed,0,10,f.speed),seed:s(g.seed,-1e6,1e6,f.seed),tint:Array.isArray(g.tint)&&g.tint.length>=3?g.tint:f.tint,epochMs:s(g.epochMs,0,9e15,0)})},stop:()=>{h&&cancelAnimationFrame(h),h=0}}}let M=null;function O(e){let t=I();if(t){if(!e){t.style.display="none",M&&(M.stop(),M=null);return}t.style.display="",M||(M=ot(P(E())))}}function L(e){let t=e&&e.background&&e.background.mode?String(e.background.mode):"perlin-nebula";O(t!=="none"),t!=="none"&&M&&M.setConfig(P(e)),tt(e)}function z(){L(E());let e=null,t=null;function n(){let o=D();o!==e&&(t&&t.disconnect(),t=null,e=o,e&&(t=new T(()=>{L(E())}),t.observe(e,{attributes:!0,attributeFilter:["data-scene-b64"]})))}n();let r=document.body||document.documentElement;r&&new T(()=>{n(),L(E())}).observe(r,{childList:!0,subtree:!0}),window.__rewindRemotePlayerBg={setEnabled:O}}document.readyState==="loading"?k(document,"DOMContentLoaded",z):z()})();})();
