"""Read PostgreSQL COPY as data, never execute supplied SQL. Export only public metadata."""
import gzip, json, re, sqlite3, sys, pathlib, time
root=pathlib.Path(__file__).resolve().parent.parent
out=root/'.data'; out.mkdir(exist_ok=True)
db=sqlite3.connect(out/'catalog.sqlite')
db.executescript('PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; CREATE TABLE IF NOT EXISTS catalog (id TEXT PRIMARY KEY, name TEXT, author TEXT, updated TEXT, pc INTEGER, quest INTEGER, rating TEXT, data TEXT); CREATE VIRTUAL TABLE IF NOT EXISTS catalog_fts USING fts5(name,author,description,tags,content="");')
esc=re.compile(r'\\([0-7]{1,3}|[btnvfr\\])')
def decode(s):
 if s==r'\N': return None
 return esc.sub(lambda m: chr(int(m[1],8)) if m[1][0].isdigit() else {'b':'\b','t':'\t','n':'\n','v':'\v','f':'\f','r':'\r','\\':'\\'}[m[1]],s)
def j(s,default):
 try:return json.loads(s) if s else default
 except:return default
batch=[]; exports=[]; total=public=0;started=time.time(); chunk=0
def flush():
 global batch,exports,chunk
 if not batch:return
 db.executemany('INSERT INTO catalog VALUES (?,?,?,?,?,?,?,?)',batch)
 # FTS uses the catalog rowid and never contains private records.
 db.executemany('INSERT INTO catalog_fts(rowid,name,author,description,tags) VALUES (?,?,?,?,?)',[(r[0],r[1],r[2],r[3],r[4]) for r in fts])
 db.commit()
 with gzip.open(out/f'chunk-{chunk:05d}.json.gz','wt',encoding='utf-8') as f:json.dump(exports,f,ensure_ascii=False,separators=(',',':'))
 chunk+=1;batch=[];exports=[];fts.clear()
fts=[]
with gzip.open(sys.argv[1],'rt',encoding='utf-8') as src:
 for line in src:
  if not line.startswith('avtr_'):continue
  total+=1
  p=line.rstrip('\n').split('\t')
  if p[5]!='public':continue
  p=list(map(decode,p));public+=1
  name=j(p[1],'')
  if not isinstance(name,str):name=str(name or '')
  packages=j(p[11],[]) or []
  native=[x for x in packages if x.get('variant')!='impostor']
  pc=any(x.get('platform')=='standalonewindows' for x in native)
  quest=any(x.get('platform')=='android' for x in native)
  ratings=[x.get('performanceRating') for x in native if x.get('platform')=='standalonewindows' and x.get('performanceRating')]
  rating=ratings[-1] if ratings else 'Unknown'
  tags=re.findall(r'"((?:[^"\\]|\\.)*)"',p[10] or '')
  if not tags and p[10] not in ('{}',None):tags=(p[10] or '').strip('{}').split(',')
  a={'id':p[0],'name':name or 'Untitled avatar','author':p[3] or 'Unknown creator','authorId':p[2],'description':(p[4] or '')[:6000],'image':p[8] or p[9] or '', 'thumbnail':p[9] or p[8] or '', 'tags':tags,'pc':pc,'quest':quest,'rating':rating,'created':p[14] or '', 'updated':p[15] or '', 'locked':p[22]=='t'}
  data=json.dumps(a,ensure_ascii=False,separators=(',',':'))
  batch.append((a['id'],a['name'],a['author'],a['updated'],int(pc),int(quest),rating,data))
  fts.append((public,a['name'],a['author'],a['description'],' '.join(tags)))
  exports.append(a)
  if len(batch)>=1000:flush()
  if public%100000==0:print(json.dumps({'public':public,'elapsed':round(time.time()-started)}),flush=True)
flush()
db.executescript('CREATE INDEX IF NOT EXISTS catalog_updated ON catalog(updated DESC); CREATE INDEX IF NOT EXISTS catalog_platform ON catalog(quest,pc,updated DESC); CREATE INDEX IF NOT EXISTS catalog_rating ON catalog(rating,updated DESC);')
stats={'total':total,'public':public,'privateExcluded':total-public,'chunks':chunk,'importedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
(out/'stats.json').write_text(json.dumps(stats),encoding='utf-8')
db.close();print(json.dumps(stats),flush=True)
