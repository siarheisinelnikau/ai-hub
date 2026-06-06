"""Connect all registered LLMs and remove retired Open WebUI providers."""
import json
from pathlib import Path
import re
import subprocess
import time
import urllib.request
import yaml

# Stable upstream API aliases. Several quantizations share an alias, so WebUI
# must namespace those connections rather than silently merge their models.
API_MODELS = {
    'glm53-flash-maya-m-derisked': 'glm-5.3-flash-derisked',
    'glm53-flash-maya-m': 'glm-5.3-flash',
    'nemotron35-super-uncensored': 'nemotron-3.5-super-vl',
    'nemotron35-super-nvfp4': 'nemotron-3.5-super-vl',
    'nemotron35-super-q4k': 'nemotron-3.5-super-vl',
    'qwen-q4k-llama': 'qwen3.8-27b',
    'qwen-q6k-llama': 'qwen3.8-27b',
    'gemma4-26b-q4k-mtp': 'gemma-4-26b',
    'qwen38-flash-next-strata': 'qwen3.8-flash-next-unsloth-ud-iq4_xs',
    'qwen38-flash-next-uncensored-strata': 'qwen3.8-flash-next-uncensored-ud-iq4_xs',
    'qwen38-flash-next-iq3-strata': 'qwen3.8-flash-next-iq3_xxs',
    'qwen38-35b-q8-llama': 'qwen3.8-35b',
    'qwen35-122b-q4k-llama': 'qwen3.5-122b',
    'gpt-oss-120b': 'gpt-oss-120b',
}
# Preserve the formerly visible aliases for existing conversations. Namespace
# the other profiles that used those same aliases.
PREFIXED = {'nemotron35-super-q4k', 'nemotron35-super-nvfp4', 'qwen-q4k-llama'}
providers = []
retired_pattern = r':(?:18020|18022|18023|18024|18025|18040)/'
registry = yaml.safe_load(Path('/srv/ai-hub/registry.yaml').read_text())
for model in registry['models']:
    if model['category'] != 'LLM':
        continue
    key = (Path('/srv/ai-hub/keys') / model['id']).read_text().strip() if model['backend'] in ('Strata', 'Maya') else 'none'
    api_id = API_MODELS[model['id']]
    prefix = model['id'] if model['id'] in PREFIXED else ''
    providers.append({'url': f'http://host.docker.internal:{model["port"]}/v1', 'key': key,
        'profile': model['id'], 'api_id': api_id, 'prefix': prefix,
        'model_id': prefix+'.'+api_id if prefix else api_id,
        'name': model['name']+' '+model['quant']})
assert {p['profile'] for p in providers} == set(API_MODELS), 'Update WebUI aliases for the model registry'
providers.append({'url':'http://host.docker.internal:8080/v1', 'key':'none', 'profile':'universal',
    'api_id':'universal', 'prefix':'', 'model_id':'universal', 'name':'AI Hub Universal'})
assert len({p['model_id'] for p in providers}) == len(providers)
unit = Path('/etc/systemd/system/ai-open-webui.service')
before = unit.read_text()
urls = re.search(r'OPENAI_API_BASE_URLS="([^"]*)"', before).group(1).split(';')
match = re.search(r'OPENAI_API_KEYS="([^"]*)"', before)
keys = match.group(1).split(';') if match else ['none'] * len(urls)
keys += ['none'] * max(0, len(urls) - len(keys))
keep = [i for i, url in enumerate(urls) if not re.search(retired_pattern, url)]
urls, keys = [urls[i] for i in keep], [keys[i] for i in keep]
for provider in providers:
    if provider['url'] not in urls:
        urls.append(provider['url']); keys.append(provider['key'])
    else:
        keys[urls.index(provider['url'])] = provider['key']
after = re.sub(r'OPENAI_API_BASE_URLS="[^"]*"', 'OPENAI_API_BASE_URLS="' + ';'.join(urls) + '"', before)
if match:
    after = re.sub(r'OPENAI_API_KEYS="[^"]*"', 'OPENAI_API_KEYS="' + ';'.join(keys) + '"', after)
else:
    after = after.replace(' -e OPENAI_API_BASE_URLS=', ' -e OPENAI_API_KEYS="' + ';'.join(keys) + '" -e OPENAI_API_BASE_URLS=')
unit.write_text(after); unit.chmod(0o600)
check = unit.read_text()
assert all(p['url'] in check and p['key'] in check for p in providers)
assert not re.search(retired_pattern, check)
code = '''
import json,sqlite3,sys,time,re
from pathlib import Path
payload=json.loads(sys.stdin.read())
providers=payload['providers']; retired_pattern=payload['retired_pattern']
c=sqlite3.connect('/app/backend/data/webui.db')
backup_path=Path('/app/backend/data/webui.pre-static-models-20261018.db')
if not backup_path.exists():
 backup=sqlite3.connect(str(backup_path)); c.backup(backup); backup.close()
def read(k,default):
 row=c.execute('select value from config where key=?',(k,)).fetchone()
 return json.loads(row[0]) if row else default
urls=read('openai.api_base_urls',[]); keys=read('openai.api_keys',[])
keys += ['none'] * max(0,len(urls)-len(keys))
keep=[i for i,u in enumerate(urls) if not re.search(retired_pattern,u)]
configs=read('openai.api_configs',{})
configs={str(new):configs[str(old)] for new,old in enumerate(keep) if str(old) in configs}
urls,keys=[urls[i] for i in keep],[keys[i] for i in keep]
for provider in providers:
 if provider['url'] not in urls:
  urls.append(provider['url']); keys.append(provider['key'])
 else: keys[urls.index(provider['url'])]=provider['key']
 idx=urls.index(provider['url'])
 config=configs.get(str(idx),configs.get(provider['url'],{})).copy()
 config.update(enable=True,model_ids=[provider['api_id']],prefix_id=provider['prefix'])
 configs[str(idx)]=config
 configs.pop(provider['url'],None)
for k,v in [('openai.api_base_urls',urls),('openai.api_keys',keys),('openai.api_configs',configs)]:
 c.execute('insert into config(key,value,updated_at) values(?,?,?) on conflict(key) do update set value=excluded.value,updated_at=excluded.updated_at',(k,json.dumps(v),int(time.time())))
c.commit()
assert all(read('openai.api_keys',[])[read('openai.api_base_urls',[]).index(p['url'])] == p['key'] for p in providers)
assert all(read('openai.api_configs',{})[str(urls.index(p['url']))]['model_ids'] == [p['api_id']] for p in providers)
assert not any(re.search(retired_pattern,u) for u in read('openai.api_base_urls',[]))
print('Verified persisted Open WebUI providers; URLs:',len(urls),'keys:',len(keys))
'''
subprocess.run(['docker', 'exec', '-i', 'open-webui', 'python', '-c', code], input=json.dumps({'providers':providers,'retired_pattern':retired_pattern}), text=True, check=True)
subprocess.run(['systemctl', 'daemon-reload'], check=True)
# The existing container may have been started outside the oneshot unit.
subprocess.run(['systemctl', 'stop', 'ai-open-webui'], check=False)
def container_exists():
    return 'open-webui' in subprocess.check_output(['docker', 'ps', '-a', '--format', '{{.Names}}'], text=True).splitlines()
if container_exists():
    # ExecStopPost may remove the container between our check and docker stop.
    for command in [['docker', 'stop', '--timeout', '20', 'open-webui'], ['docker', 'rm', 'open-webui']]:
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode and container_exists():
            raise RuntimeError('Open WebUI container could not be stopped/removed')
assert not container_exists()
subprocess.run(['systemctl', 'start', 'ai-open-webui'], check=True)
deadline = time.monotonic()+180
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen('http://127.0.0.1:3000/health',timeout=5) as response:
            if response.status == 200:
                break
    except (OSError,ValueError):
        pass
    time.sleep(2)
else:
    raise TimeoutError('Open WebUI did not become healthy')

# Use the application's API to add display metadata, preserving any names and
# parameters that the user has already edited. No new users or credentials.
code = r'''
import datetime,json,os,sqlite3,sys,time,urllib.request,uuid
from pathlib import Path
import jwt
providers=json.loads(sys.stdin.read())
c=sqlite3.connect('/app/backend/data/webui.db')
admins=c.execute("select id from user where role='admin' order by created_at").fetchall()
assert admins,'Open WebUI requires an existing administrator'
env=dict(v.split(b'=',1) for v in Path('/proc/1/environ').read_bytes().split(b'\0') if b'=' in v)
secret=env.get(b'WEBUI_SECRET_KEY',b'').decode() or os.getenv('WEBUI_SECRET_KEY','')
if not secret:
 secret=Path('/app/backend/.webui_secret_key').read_text().strip()
now=int(time.time())
token=jwt.encode({'id':admins[0][0],'iat':now,'exp':now+300,'jti':str(uuid.uuid4())},secret,algorithm='HS256')
for p in providers:
 if c.execute('select 1 from model where id=?',(p['model_id'],)).fetchone():continue
 data={'id':p['model_id'],'base_model_id':None,'name':p['name'],'params':{},'meta':{
  'description':'Start this model in AI Hub before chatting.' if p['profile']!='universal' else 'Follows the last-started running model in AI Hub.'}}
 req=urllib.request.Request('http://127.0.0.1:8080/api/v1/models/create',data=json.dumps(data).encode(),
  headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
 with urllib.request.urlopen(req,timeout=30) as response:
  result=json.load(response);assert result['id']==p['model_id']
print('Verified readable model names; existing user customizations preserved')
'''
subprocess.run(['docker','exec','-i','open-webui','python','-c',code],input=json.dumps(providers),text=True,check=True)
mapping = {'version':1,'models':{p['profile']:{'id':p['model_id'],'name':p['name']} for p in providers}}
path=Path('/srv/ai-hub/static/webui-models.json')
temporary=path.with_suffix('.json.new')
temporary.write_text(json.dumps(mapping,ensure_ascii=False,indent=2)+'\n');temporary.chmod(0o644);temporary.replace(path)
assert json.loads(path.read_text()) == mapping
print('Verified persistent offline-visible model list, unique aliases, display names and Chat model map')
