(()=>{function s(M,y,_,F){let f=Number(M);return Number.isFinite(f)?Math.max(y,Math.min(_,f)):F}function R(M){if(typeof M!="string")return null;let y=M.trim().match(/^(\d+(?:\.\d+)?)\s*:\s*(\d+(?:\.\d+)?)$/);if(!y)return null;let _=s(y[1],.001,1e3,0),F=s(y[2],.001,1e3,0),f=_/F;return!Number.isFinite(f)||f<=0?null:f}(function(){let M="remote-player-bg-canvas",y="player-scene",_="remote-player-video-frame",F=1.7777777777777777,f={speed:1,seed:0,tint:[1,1,1]};function $(){return document.getElementById(M)}function L(){return document.getElementById(y)}function P(){return document.getElementById(_)}function E(){let e=L();if(!e||!e.dataset)return null;let t=e.dataset.sceneB64;if(!t)return null;try{let n=atob(t);return JSON.parse(n)}catch{return null}}function I(e){if(typeof e!="string")return null;let t=e.trim();if(t.startsWith("#")&&(t=t.slice(1)),t.length===3&&(t=t.split("").map(i=>i+i).join("")),!/^[0-9a-fA-F]{6}$/.test(t))return null;let n=parseInt(t.slice(0,2),16)/255,o=parseInt(t.slice(2,4),16)/255,r=parseInt(t.slice(4,6),16)/255;return[n,o,r]}function z(e){let t=I(e);if(t)return t;if(Array.isArray(e)&&e.length>=3){let n=Number(e[0]),o=Number(e[1]),r=Number(e[2]);if(![n,o,r].every(Number.isFinite))return null;let i=n>1||o>1||r>1?255:1;return[Math.max(0,Math.min(1,n/i)),Math.max(0,Math.min(1,o/i)),Math.max(0,Math.min(1,r/i))]}return null}let T=null;function O(e){if(typeof e!="string")return null;let t=e.match(/rgba?\(\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)\s*,\s*(\d+(?:\.\d+)?)/i);if(t){let o=s(t[1],0,255,255)/255,r=s(t[2],0,255,255)/255,i=s(t[3],0,255,255)/255;return[o,r,i]}let n=e.match(/color\(\s*srgb\s+([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)/i);if(n){let o=s(n[1],0,1,1),r=s(n[2],0,1,1),i=s(n[3],0,1,1);return[o,r,i]}return null}function H(){if(T)return T;if(!document.body)return null;let e=document.createElement("span");return e.style.position="absolute",e.style.left="-9999px",e.style.top="-9999px",e.style.visibility="hidden",document.body.appendChild(e),T=e,e}function G(e,t,n){try{if(!window.CSS||typeof CSS.supports!="function"||!CSS.supports("color","oklch(50% 0.1 120)"))return null;let o=H();if(!o)return null;let r=s(e,0,1,1)*100,i=s(t,0,1,0),l=s(n,0,360,0);return o.style.color=`oklch(${r}% ${i} ${l})`,O(getComputedStyle(o).color)}catch{return null}}function U(e,t,n){let o=s(e,0,1,1),r=s(t,0,1,0),i=s(n,0,360,0)*Math.PI/180,l=r*Math.cos(i),u=r*Math.sin(i),c=o+.3963377774*l+.2158037573*u,a=o-.1055613458*l-.0638541728*u,h=o-.0894841775*l-1.291485548*u,p=c*c*c,b=a*a*a,d=h*h*h,w=4.0767416621*p-3.3077115913*b+.2309699292*d,m=-1.2684380046*p+2.6097574011*b-.3413193965*d,x=-.0041960863*p-.7034186147*b+1.707614701*d,S=A=>(A=Math.max(0,Math.min(1,A)),A<=.0031308?12.92*A:1.055*Math.pow(A,1/2.4)-.055);return[S(w),S(m),S(x)]}function K(e,t,n){return G(e,t,n)||U(e,t,n)}function k(e){let t=e&&e.background?e.background:null,n=s(t&&t.speed,0,10,f.speed),o=s(t&&t.seed,-1e6,1e6,f.seed),r=null;if(t&&t.tint_oklch&&typeof t.tint_oklch=="object"){let l=t.tint_oklch.l,u=t.tint_oklch.c,c=t.tint_oklch.h;r=K(l,u,c)}r||(r=z(t&&t.tint));let i=s(t&&t.epoch_ms,0,9e15,0);return{speed:n,seed:o,tint:r||f.tint,epochMs:i}}let g={x:.5,y:.5,width:.9,height:.9,aspect:"",border:{enabled:!0,size:2,opacity:.1}};function V(e){let t=e&&e.stage?e.stage:null;return R(t&&t.aspect)||F}function W(e,t,n,o){let l=e*o/n,u=t*n/o,c=e,a=l;if(Number.isFinite(l)&&Number.isFinite(u)){let d=Math.abs(l-t);Math.abs(u-e)<d&&(c=u,a=t)}if(!Number.isFinite(c)||!Number.isFinite(a)||c<=0||a<=0)return{width:e,height:t};let h=Math.min(1,1/c,1/a);c*=h,a*=h;let p=Math.max(1,.1/c,.1/a);c*=p,a*=p;let b=Math.min(1,1/c,1/a);return c*=b,a*=b,{width:c,height:a}}function q(e){let t=Math.max(1,window.innerWidth||1),n=Math.max(1,window.innerHeight||1);if(t/n>=e){let u=n,c=u*e;return{left:(t-c)/2,top:0,width:c,height:u}}let r=t,i=r/e;return{left:0,top:(n-i)/2,width:r,height:i}}function j(e){let t=e&&e.video?e.video:null,n=t&&t.border?t.border:null;return{x:s(t&&t.x,0,1,g.x),y:s(t&&t.y,0,1,g.y),width:s(t&&(t.width??t.w),.05,1,g.width),height:s(t&&(t.height??t.h),.05,1,g.height),aspect:typeof(t&&t.aspect)=="string"?t.aspect:g.aspect,border:{enabled:!!(n&&typeof n.enabled<"u"?n.enabled:g.border.enabled),size:s(n&&n.size,0,50,g.border.size),opacity:s(n&&n.opacity,0,1,g.border.opacity)}}}function Y(e){let t=P();if(!t)return;let n=j(e),o=V(e),r=q(o),i=n.width,l=n.height,u=R(n.aspect);if(u){let b=W(i,l,u,o);i=b.width,l=b.height}let c=r.left+n.x*r.width,a=r.top+n.y*r.height,h=i*r.width,p=l*r.height;t.style.left=`${c.toFixed(1)}px`,t.style.top=`${a.toFixed(1)}px`,t.style.width=`${h.toFixed(1)}px`,t.style.height=`${p.toFixed(1)}px`,t.style.transform="translate(-50%, -50%)",n.border.enabled&&n.border.size>0&&n.border.opacity>0?(t.style.borderStyle="solid",t.style.borderWidth=Math.round(n.border.size)+"px",t.style.borderColor=`rgba(255,255,255,${n.border.opacity})`):t.style.borderWidth="0px"}function N(e,t,n){let o=e.createShader(t);return!o||(e.shaderSource(o,n),e.compileShader(o),!e.getShaderParameter(o,e.COMPILE_STATUS))?null:o}function J(e,t,n){let o=N(e,e.VERTEX_SHADER,t),r=N(e,e.FRAGMENT_SHADER,n);if(!o||!r)return null;let i=e.createProgram();return!i||(e.attachShader(i,o),e.attachShader(i,r),e.linkProgram(i),!e.getProgramParameter(i,e.LINK_STATUS))?null:i}let X=`
    attribute vec2 a_position;
    varying vec2 v_uv;
    void main() {
      v_uv = a_position * 0.5 + 0.5;
      gl_Position = vec4(a_position, 0.0, 1.0);
    }
  `,Q=`
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
  `;function Z(e){let t=$();if(!t)return null;let n=t.getContext("webgl",{alpha:!1,antialias:!1,depth:!1,stencil:!1,premultipliedAlpha:!1,preserveDrawingBuffer:!1,powerPreference:"high-performance"});if(!n)return null;let o=J(n,X,Q);if(!o)return null;let r=n.getAttribLocation(o,"a_position"),i=n.getUniformLocation(o,"u_resolution"),l=n.getUniformLocation(o,"u_time"),u=n.getUniformLocation(o,"u_seed"),c=n.getUniformLocation(o,"u_tint"),a=n.createBuffer();n.bindBuffer(n.ARRAY_BUFFER,a),n.bufferData(n.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),n.STATIC_DRAW);function h(){let m=Math.min(2,window.devicePixelRatio||1),x=Math.max(1,Math.floor(t.clientWidth*m)),S=Math.max(1,Math.floor(t.clientHeight*m));(t.width!==x||t.height!==S)&&(t.width=x,t.height=S,n.viewport(0,0,x,S))}let p=0,b=Date.now(),d={speed:e&&e.speed||f.speed,seed:e&&e.seed||f.seed,tint:e&&e.tint||f.tint,epochMs:e&&e.epochMs||0};function w(m){if(p=requestAnimationFrame(w),document.visibilityState==="hidden")return;h(),n.useProgram(o),n.enableVertexAttribArray(r),n.bindBuffer(n.ARRAY_BUFFER,a),n.vertexAttribPointer(r,2,n.FLOAT,!1,0,0),n.uniform2f(i,t.width,t.height);let x=d.epochMs&&d.epochMs>0?d.epochMs:b;n.uniform1f(l,(Date.now()-x)/1e3*(d.speed||0)),n.uniform1f(u,d.seed||0),n.uniform3f(c,d.tint[0],d.tint[1],d.tint[2]),n.drawArrays(n.TRIANGLE_STRIP,0,4)}return window.addEventListener("resize",h,{passive:!0}),document.addEventListener("visibilitychange",()=>{document.visibilityState==="visible"&&(b=Date.now())}),h(),p=requestAnimationFrame(w),{setConfig:m=>{m&&(d={speed:s(m.speed,0,10,f.speed),seed:s(m.seed,-1e6,1e6,f.seed),tint:Array.isArray(m.tint)&&m.tint.length>=3?m.tint:f.tint,epochMs:s(m.epochMs,0,9e15,0)})},stop:()=>{p&&cancelAnimationFrame(p),p=0}}}let v=null;function D(e){let t=$();if(t){if(!e){t.style.display="none",v&&(v.stop(),v=null);return}t.style.display="",v||(v=Z(k(E())))}}function C(e){let t=e&&e.background&&e.background.mode?String(e.background.mode):"perlin-nebula";D(t!=="none"),t!=="none"&&v&&v.setConfig(k(e)),Y(e)}function B(){C(E());let e=null,t=null;function n(){let r=L();r!==e&&(t&&t.disconnect(),t=null,e=r,e&&(t=new MutationObserver(()=>{C(E())}),t.observe(e,{attributes:!0,attributeFilter:["data-scene-b64"]})))}n();let o=document.body||document.documentElement;o&&new MutationObserver(()=>{n(),C(E())}).observe(o,{childList:!0,subtree:!0}),window.__rewindRemotePlayerBg={setEnabled:D}}document.readyState==="loading"?document.addEventListener("DOMContentLoaded",B):B()})();})();
