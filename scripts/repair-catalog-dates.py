"""Repair a v1 import's timestamp mapping from the untouched PostgreSQL dump."""
import gzip,json,pathlib,sqlite3,sys,time
root=pathlib.Path(__file__).resolve().parent.parent
folder=root/'.data';db=sqlite3.connect(folder/'catalog.sqlite')
db.execute('PRAGMA synchronous=NORMAL')
started=time.time();count=0
def records():
 with gzip.open(sys.argv[1],'rt',encoding='utf-8') as f:
  for line in f:
   if line.startswith('avtr_'):
    fields=line.rstrip('\n').split('\t')
    if fields[5]=='public':yield fields
source=records()
for index,path in enumerate(sorted(folder.glob('chunk-*.json.gz'))):
 with gzip.open(path,'rt',encoding='utf-8') as f:rows=json.load(f)
 updates=[]
 for a in rows:
  p=next(source);assert p[0]==a['id'],'Source order mismatch'
  a['created']='' if p[14]==r'\N' else p[14]
  a['updated']='' if p[15]==r'\N' else p[15]
  a['locked']=p[22]=='t'
  updates.append((a['updated'],json.dumps(a,ensure_ascii=False,separators=(',',':')),a['id']))
 db.executemany('UPDATE catalog SET updated=?,data=? WHERE id=?',updates);db.commit()
 with gzip.open(path,'wt',encoding='utf-8') as f:json.dump(rows,f,ensure_ascii=False,separators=(',',':'))
 count+=len(rows)
 if index%100==0:print(json.dumps({'corrected':count,'elapsed':round(time.time()-started)}),flush=True)
assert next(source,None) is None
db.close();print(json.dumps({'complete':True,'corrected':count,'elapsed':round(time.time()-started)}),flush=True)
