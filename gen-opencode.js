const {serviceURL, hubURL, hubToken} = require('./scripts/env.cjs');
// Generates ~/.config/opencode/opencode.jsonc from the ai-hub model matrix.
// One provider per hub LLM (each has its own port), only its largest context.
const fs=require('fs');
const os=require('os');
const path=require('path');

const THINKING={
  "thinking-xhigh":{options:{chat_template_kwargs:{enable_thinking:true,reasoning_effort:"xhigh",preserve_thinking:true}}},
  "thinking-medium":{options:{chat_template_kwargs:{enable_thinking:true,reasoning_effort:"medium",preserve_thinking:true}}},
  "thinking-low":{options:{chat_template_kwargs:{enable_thinking:true,reasoning_effort:"low",preserve_thinking:true}}},
  "no-thinking":{options:{chat_template_kwargs:{enable_thinking:false,preserve_thinking:false}}},
};
// Qwen3.5/3.8-35B templates have NO reasoning_effort levels: thinking is binary
// (enable_thinking true/false). "thinking-max" = the model's maximum = ON.
const THINKING_BINARY={
  "thinking-max":{options:{chat_template_kwargs:{enable_thinking:true}}},
  "no-thinking":{options:{chat_template_kwargs:{enable_thinking:false}}},
};
const THINKING_NEMOTRON={
  "thinking-max":{chat_template_kwargs:{enable_thinking:true}},
  "no-thinking":{chat_template_kwargs:{enable_thinking:false}},
};
const THINKING_STRATA=Object.fromEntries(["none","low","medium","high"].map(e=>[e==="none"?"no-thinking":"thinking-"+e,{reasoningEffort:e}]));
const THINKING_MAYA=Object.fromEntries(["none","low","high","max"].map(e=>[e==="none"?"no-thinking":"thinking-"+e,{reasoningEffort:e}]));

// [key, port, apiId, family, namePrefix, quant, [ [ctxTokens, suffixKeySuffix, output] ... ] (largest first), hasVariants]
const P=[
  ["glm53-flash-maya-m-derisked",18038,"glm-5.3-flash-derisked","glm","GLM 5.3 Flash 321B Derisked","Maya-M IQ2_S",[[1048576,"",16384]],"maya"],
  ["glm53-flash-maya-m",18037,"glm-5.3-flash","glm","GLM 5.3 Flash 321B","Maya-M IQ2_S",[[262144,"",16384]],"maya"],
	["nemotron35-super-uncensored",18036,"nemotron-3.5-super-vl","nemotron","Nemotron 3.5 Super VL 120B Uncensored","AD-Q4_K_S",[[1048576,"",32768]],"binary"],
	["nemotron35-super-nvfp4",18035,"nemotron-3.5-super-vl","nemotron","Nemotron 3.5 Super VL 120B","NVFP4",[[393114,"",32768]],"binary"],
	["nemotron35-super-q4k",18034,"nemotron-3.5-super-vl","nemotron","Nemotron 3.5 Super VL 120B","Q4K",[[1048576,"",32768]],"binary"],
	["qwen35-122b-q4k",18028,"qwen3.5-122b","qwen3","Qwen 3.5 122B","Q4K",[[262144,"",32768]],"binary"],
	["qwen38-35b-q8",18027,"qwen3.8-35b","qwen3","Qwen 3.8 35B","Q8",[[262144,"",32768]],"binary"],
	["gpt-oss-120b",18041,"gpt-oss-120b","gptoss","GPT-OSS 120B","MXFP4",[[131072,"",32768]],"gptoss"],
	["qwen38-flash-next-iq3-strata",18033,"qwen3.8-flash-next-iq3_xxs","qwen3","Qwen 3.8 Flash-Next 125B","IQ3_XXS",[[262144,"",16384]],"strata"],
  ["qwen38-flash-next-uncensored-strata",18030,"qwen3.8-flash-next-uncensored-ud-iq4_xs","qwen3","Qwen 3.8 Flash-Next 125B Uncensored","UD-IQ4_XS",[[262144,"",16384]],"strata"],
  ["qwen38-flash-next-strata",18029,"qwen3.8-flash-next-unsloth-ud-iq4_xs","qwen3","Qwen 3.8 Flash-Next 125B","UD-IQ4_XS",[[262144,"",16384]],"strata"],
  ["qwen-q4k",18021,"qwen3.8-27b","qwen3","Qwen 3.8 27B","Q4K",[[262144,"",32768]],true],
  ["qwen-q6k",18026,"qwen3.8-27b","qwen3","Qwen 3.8 27B","Q6K",[[262144,"",32768]],true],
  ["gemma4-26b-q4k-mtp",18031,"gemma-4-26b","gemma4","Gemma 4 26B","Q4K",[[262144,"",32768]],true],
];

const k=(n)=>n>=1048576?(n/1048576)+"M":n>=1024?Math.round(n/1024)+"k":n+"";
// Universal follows the last-started running hub model; keep per-model entries.
const DEFAULT_MODEL="universal/universal";
// Default thinking variant: xhigh is the MAX supported by the Qwen 3.8 chat template
// (only xhigh/medium/low — verified from tokenizer.chat_template in the GGUFs).
const DEFAULT_VARIANT="thinking-xhigh";
const provider={};
for(const[key,port,apiId,family,namePrefix,quant,variants,hasV]of P){
  const models={};
  for(const[ctx,suffix,out]of variants){
    models[apiId+suffix]={
      id:apiId,
      name:`${namePrefix} ${quant} ${k(ctx)}`,
      family,
      reasoning:true,
      tool_call:true,
      temperature:true,
      ...(hasV==="maya"?{modalities:{input:["text","image"],output:["text"]}}:{}),
      limit:{context:ctx,output:out},
      ...(hasV==="maya"?{variants:THINKING_MAYA,defaultVariant:"thinking-high"}
        :hasV==="strata"?{variants:THINKING_STRATA,defaultVariant:"thinking-high"}
        :hasV==="gptoss"?{variants:Object.fromEntries(["low","medium","high"].map(e=>["thinking-"+e,{reasoningEffort:e}])),defaultVariant:"thinking-high"}
        :hasV==="binary"?{variants:family==="nemotron"?THINKING_NEMOTRON:THINKING_BINARY,defaultVariant:"thinking-max"}
        :hasV?{variants:THINKING,defaultVariant:DEFAULT_VARIANT}:{}),
    };
  }
  provider[key]={
    name:"AI HUB (Local)",
    options:{apiKey:"none",baseURL:serviceURL(port),timeout:false,...(key.startsWith("nemotron35-super-")?{headerTimeout:7200000}:{}),chunkTimeout:["nemotron35-super-q4k","nemotron35-super-uncensored"].includes(key)?7200000:1800000},
    models,
  };
  if(hasV==="strata" || hasV==="maya") {
    const keyName=key==="glm53-flash-maya-m-derisked"?"maya-glm53-derisked-api-key":hasV==="maya"?"maya-glm53-api-key":key==="qwen38-flash-next-iq3-strata"?"strata-iq3-api-key":key==="qwen38-flash-next-uncensored-strata"?"strata-uncensored-iq4-api-key":"strata-api-key";
    const keyFile=path.join(os.homedir(),".config","ai-hub",keyName);
    if(!fs.existsSync(keyFile)) throw new Error("Missing private Strata API key: "+keyFile);
    provider[key].options.apiKey=fs.readFileSync(keyFile,"utf8").trim();
  }
}
// The configured 1M budget does not enlarge the active model's loaded context.
provider.universal={
  name:"AI HUB (Universal)",
  npm:"@ai-sdk/openai-compatible",
  options:{apiKey:hubToken(),baseURL:hubURL(),timeout:false,headerTimeout:7200000,chunkTimeout:7200000},
  models:{universal:{id:"universal",name:"AI Hub Universal 1M",family:"ai-hub",reasoning:false,tool_call:true,temperature:true,limit:{context:1048576,output:32768}}},
};
const doc={"$schema":"https://opencode.ai/config.json",model:DEFAULT_MODEL,provider};
const target=process.env.AIHUB_OPENCODE_OUTPUT || path.join(os.homedir(),".config","opencode","opencode.jsonc");
fs.writeFileSync(target,JSON.stringify(doc,null,2)+"\n");
console.log("wrote",target);
const j=JSON.parse(fs.readFileSync(target,"utf8"));
let n=0;
for(const[key,p]of Object.entries(j.provider)){
  const names=Object.values(p.models).map(m=>m.name);
  n+=names.length;
  console.log(p.name," :"+p.options.baseURL.split(":")[2]+" -> "+names.join(" | "));
}
console.log("TOTAL:",n,"models; 32k left:",(JSON.stringify(j).match(/32k/g)||[]).length);
