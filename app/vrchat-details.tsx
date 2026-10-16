import VRChatGate from './vrchat-gate';
import UserStatus from './user-status';
import {entityPath,copyEntityLink} from './routes';
import {api} from './api';
import {formatDate,formatNumber,t as tr,useLocale} from './i18n';
import {useState,useEffect,useRef} from 'react';
import {Reviews} from './cove-account';
import {AddToCollection} from './cove-social';
import {Users,Globe2,UserRound,ExternalLink,LoaderCircle,Send,Play,UserPlus,UserMinus,MapPin,ChevronLeft,Copy,Maximize2,ShieldCheck,BadgeCheck,LogIn,LogOut,Braces,Download} from 'lucide-react';
export type Kind='world'|'user'|'group';
export type RawKind=Kind|'avatar';
type Target={kind:Kind;id:string};
export function Photo({url,fallbackUrl,name,alt=''}:any){useLocale();
 const sources=[...new Set<string>([url,fallbackUrl].filter(Boolean))];
 const requestKey=JSON.stringify(sources);
 const [attempt,setAttempt]=useState({requestKey,index:0});
 const index=attempt.requestKey===requestKey?attempt.index:0;
 const current=sources[index];
 if(current)return <img key={requestKey+index} src={'/api/image?url='+encodeURIComponent(current)} alt={alt} onError={()=>setAttempt({requestKey,index:index+1})} referrerPolicy="no-referrer" loading="lazy"/>;
 return name?<div className="initial-fallback" aria-hidden="true">{[...String(name).trim()][0]?.toUpperCase()||'?'}</div>:<div className="image-fallback"><UserRound strokeWidth={.8}/><span>{tr("Preview unavailable")}</span></div>;
}
const date=(s?:string)=>s&&Number.isFinite(new Date(s).getTime())?formatDate(/^\d{4}-\d{2}-\d{2}$/.test(s)?s+'T12:00:00':s):'-';
const count=(n?:number)=>typeof n==='number'?formatNumber(n):'?';
const regionNames:Record<string,string>={us:'US West',use:'US East',eu:'Europe',jp:'Japan'};
const regionOf=(instance:string)=>/~region\(([a-z]+)\)/.exec(instance)?.[1]||'us';
const platformName=(p:string)=>p==='standalonewindows'?'PC':p==='android'?'Android / Quest':p==='ios'?'iOS':p;
const safeLinks=(links:unknown)=>(Array.isArray(links)?links:[]).filter((l:unknown):l is string=>typeof l==='string'&&/^https:\/\//.test(l));
const joinStates:Record<string,string>={open:'Anyone can join',request:'Join by request',invite:'Invite only',closed:'Closed'};
function trustRank(tags:string[]=[]){if(tags.includes('system_trust_veteran'))return 'Trusted User';if(tags.includes('system_trust_trusted'))return 'Known User';if(tags.includes('system_trust_known'))return 'User';if(tags.includes('system_trust_basic'))return 'New User';return 'Visitor';}
const userImage=(u:any)=>u.profilePicOverride||u.userIcon||u.currentAvatarImageUrl||u.currentAvatarThumbnailImageUrl;
const userThumb=(u:any)=>u.userIcon||u.profilePicOverride||u.currentAvatarThumbnailImageUrl;
export const detailHref=(kind:Kind,id:string)=>entityPath(kind,id);
/* Quick-view modal from anywhere; DetailHost (mounted once in Atlas) renders it. Full pages live at /world/<id>, /user/<id>, /group/<id>. */
export function openDetail(kind:Kind,id:string){window.dispatchEvent(new CustomEvent<Target>('vrcove:detail',{detail:{kind,id}}));}
/* A plain click opens the quick view; modified clicks (new tab, etc.) and page-mode links go to the full page. */
export function detailLink(kind:Kind,id:string,page=false){return {href:detailHref(kind,id),onClick:(e:any)=>{if(page||e.metaKey||e.ctrlKey||e.shiftKey||e.altKey||e.button)return;e.preventDefault();openDetail(kind,id);}};}
function useLoad(path:string|null,user:any){
 const [state,setState]=useState<{path:string|null;data:any;error:string}>({path:null,data:null,error:''});
 useEffect(()=>{if(!path||!user)return;let alive=true;api(path).then(data=>{if(alive)setState({path,data,error:''});}).catch(e=>{if(alive)setState({path,data:null,error:e.message});});return()=>{alive=false;};},[path,user?.id]);
 return state.path===path?state:{path,data:null,error:''};
}
/* Full, unfiltered VRChat JSON for an object, fetched only when opened. `local` adds another record to inspect (e.g. the catalog snapshot). */
export function RawJson({kind,id,user,notify,local,localLabel='Our database'}:{kind:RawKind;id:string;user:any;notify:(s:string)=>void;local?:unknown;localLabel?:string}){useLocale();
 const [open,setOpen]=useState(false),[tab,setTab]=useState<'live'|'local'>(user?'live':'local');
 const live=useLoad(open&&user&&tab==='live'?'/account/raw?kind='+kind+'&id='+encodeURIComponent(id):null,user);
 const value=tab==='local'?local:live.data;const text=value===undefined||value===null?'':JSON.stringify(value,null,2);
 function download(){const url=URL.createObjectURL(new Blob([text],{type:'application/json'})),a=document.createElement('a');a.href=url;a.download=id+(tab==='local'?'.catalog':'')+'.json';a.click();URL.revokeObjectURL(url);}
 return <details className="raw-json" onToggle={e=>setOpen((e.target as HTMLDetailsElement).open)}><summary>{kind==='avatar'?<><Download size={14}/>{tr("View metadata")}</>:<><Braces size={14}/>{tr("View metadata")}</>}</summary>
  {open&&<div className="raw-json-body"><div className="raw-json-bar">{local!==undefined&&<div className="chips"><button className={tab==='live'?'chosen':''} onClick={()=>setTab('live')}>{tr("Live VRChat")}</button><button className={tab==='local'?'chosen':''} onClick={()=>setTab('local')}>{localLabel}</button></div>}<span/>{text&&<><button className="standard-button" onClick={()=>navigator.clipboard.writeText(text).then(()=>notify(tr("JSON copied"))).catch(()=>notify(tr("Clipboard access is unavailable.")))}><Copy size={13}/>{tr("Copy")}</button><button className="standard-button" onClick={download}><Download size={13}/>{tr("Download")}</button></>}</div>
   {tab==='live'&&!user?<p className="fineprint">{tr("Connect your VRChat account to load the live record.")}</p>:tab==='live'&&live.error?<p className="fineprint">{live.error}</p>:text?<pre>{text}</pre>:<div className="loading-inline"><LoaderCircle size={14} className="spin"/>{tr("Loading…")}</div>}</div>}
 </details>;
}
function Section({title,state,empty,children}:any){useLocale();
 if(state.error)return <section className="entity-section"><h3>{title}</h3><p className="fineprint">{state.error}</p></section>;
 if(!state.data)return <section className="entity-section"><h3>{title}</h3><div className="loading-inline"><LoaderCircle size={14} className="spin"/>{tr("Loading…")}</div></section>;
 if(!state.data.length)return empty?<section className="entity-section"><h3>{title}</h3><p className="fineprint">{empty}</p></section>:null;
 return <section className="entity-section"><h3>{title}<small>{state.data.length}</small></h3>{children}</section>;
}
export function DetailView({coveAccount,kind,id,user,onLogin,notify,mode,onBack,onClose}:{coveAccount:any;kind:Kind;id:string;user:any;onLogin:()=>void;notify:(s:string)=>void;mode:'page'|'modal';onBack?:()=>void;onClose?:()=>void}){useLocale();
 const page=mode==='page';
 const main=useLoad(user?'/account/'+kind+'?id='+id:null,user);
 const [patch,setPatch]=useState<any>({}),[busy,setBusy]=useState(''),[confirm,setConfirm]=useState<any>(null),[region,setRegion]=useState('us');
 useEffect(()=>{setPatch({});setConfirm(null);},[kind,id]);
 useEffect(()=>{if(page&&main.data)window.scrollTo(0,0);},[page,kind,id,!!main.data]);
 const data=main.data?{...main.data,...patch}:null;
 // Secondary data: the profile's current world everywhere; lists only on full pages.
 const place=useLoad(kind==='user'&&data?.location?.startsWith('wrld_')?'/account/world?id='+data.location.split(':')[0]:null,user);
 const userWorlds=useLoad(page&&data&&(kind==='user'||kind==='world')?'/account/user-worlds?id='+(kind==='user'?id:data.authorId):null,user);
 const userGroups=useLoad(page&&kind==='user'&&data?'/account/user-groups?id='+id:null,user);
 const posts=useLoad(page&&kind==='group'&&data?'/account/group-posts?id='+id:null,user);
 const members=useLoad(page&&kind==='group'&&data?'/account/group-members?id='+id:null,user);
 const groupInstances=useLoad(page&&kind==='group'&&data?.membershipStatus==='member'?'/account/group-instances?id='+id:null,user);
 const owner=useLoad(kind==='group'&&data?.ownerId?'/account/user?id='+data.ownerId:null,user);
 const link=(k:Kind,i:string)=>page?{href:detailHref(k,i)}:detailLink(k,i);
 async function act(action:string,target:string,done:string,extra:object={}){setBusy(action+':'+target);try{const r=await api('/account/action',{action,id:target,...extra});notify(done);setConfirm(null);return r??{};}catch(e){notify((e as Error).message);}finally{setBusy('');}}
 async function startInstance(){const r=await act('newInstance',id,'Instance created.',{region});if(r?.location)await act('selfInvite',r.location,'Invite sent. Check VRChat to join.');}
 const instanceRow=(location:string,label:string,users?:number,capacity?:number)=><div className="instance-row" key={location}><div><strong>{label}</strong><small>{regionNames[regionOf(location)]||regionOf(location)}{users!==undefined&&<> · <Users size={11}/>{users}{capacity?' / '+capacity:''}</>}</small></div><button className="standard-button" disabled={!!busy} onClick={()=>act('selfInvite',location,'Invite sent. Check VRChat to join.')}>{busy==='selfInvite:'+location?<LoaderCircle size={13} className="spin"/>:<Send size={13}/>}{tr("Invite me")}</button><a className="standard-button" href={'vrchat://launch?ref=vrcove&id='+encodeURIComponent(location)} aria-label={'Launch '+label+' in VRChat'}><Play size={13}/>{tr("Launch")}</a></div>;
 const confirmBox=confirm&&<div className="detail-confirm" role="alertdialog" aria-label={tr("Confirm action")}><p>{confirm.text}</p><button className="primary" disabled={!!busy} onClick={async()=>{const r=await act(confirm.action,confirm.id,confirm.done);if(r)setPatch((p:any)=>({...p,...confirm.patch}));}}>{tr("Confirm")}</button><button className="standard-button" onClick={()=>setConfirm(null)}>{tr("Cancel")}</button></div>;
 const toolbar=<div className="detail-toolbar"><button className="text-link" onClick={()=>copyEntityLink(kind,id).then(()=>notify(tr("Link copied"))).catch(()=>notify(tr("Clipboard access is unavailable.")))}><Copy size={13}/>{tr("Copy link")}</button>{onBack&&<button className="text-link" onClick={onBack}><ChevronLeft size={14}/>{tr("Back")}</button>}{!page&&<a className="text-link" href={detailHref(kind,id)} onClick={()=>onClose?.()}><Maximize2 size={13}/>{tr("Open full page")}</a>}</div>;
 const idRow=<div className="avatar-id"><code>{id}</code><button aria-label={tr("Copy ID")} onClick={()=>navigator.clipboard.writeText(id).then(()=>notify(tr("ID copied"))).catch(()=>notify(tr("Clipboard access is unavailable.")))}><Copy size={14}/></button></div>;
 if(!user)return <div className={page?'entity-page':'confirm-content'}>{toolbar}<VRChatGate coveAccount={coveAccount} section={tr(kind==='world'?"this world":kind==='user'?"this profile":"this group")} onConnect={onLogin}/>{page&&<Reviews kind={kind} id={id}/>}</div>;
 if(!data)return <div className={page?'entity-page':'confirm-content'}>{toolbar}{main.error?<div className="empty-state"><h3>{tr("Unavailable")}</h3><p>{main.error}</p></div>:<div className="loading-state"><LoaderCircle className="spin"/>{tr("Loading from VRChat…")}</div>}</div>;

 let image,eyebrow,title,subtitle,body,meta,actions,extras;
 if(kind==='world'){const w=data;
  image=<Photo url={w.imageUrl||w.thumbnailImageUrl} alt=""/>;
  eyebrow=<><Globe2 size={13}/>{tr("WORLD")}{w.releaseStatus&&w.releaseStatus!=='public'?' / '+w.releaseStatus.toUpperCase():''}</>;
  title=w.name;subtitle=<>{tr("by")} {w.authorId?<a {...link('user',w.authorId)}>{w.authorName}</a>:w.authorName}</>;
  body=<p className="description">{w.description||tr("The creator hasn’t added a description.")}</p>;
  meta=<><span>{tr("Players online")}<strong>{count(w.occupants)} ({count(w.publicOccupants)} {tr("public,")} {count(w.privateOccupants)} {tr("private)")}</strong></span><span>{tr("Capacity")}<strong>{count(w.capacity)}{w.recommendedCapacity?' · recommended '+w.recommendedCapacity:''}</strong></span><span>{tr("Visits")}<strong>{count(w.visits)}</strong></span><span>{tr("Favorites")}<strong>{count(w.favorites)}</strong></span><span>{tr("Platforms")}<strong>{w.platforms?.length?w.platforms.map(platformName).join(', '):'-'}</strong></span><span>{tr("Published")}<strong>{date(w.publicationDate&&w.publicationDate!=='none'?w.publicationDate:w.created)}</strong></span><span>{tr("Last updated")}<strong>{date(w.updated)}</strong></span></>;
  const tags=(w.tags||[]).filter((t:string)=>t.startsWith('author_tag_'));
  actions=<>{tags.length>0&&<div className="tags">{tags.map((t:string)=><span key={t}>{t.replace(/^author_tag_/,'').replaceAll('_',' ')}</span>)}</div>}
   <div className="detail-section"><h3>{tr("Public instances")}{w.instances?.length>0&&<small>{w.instances.length}</small>}</h3>{w.instances?.length?<div className="instance-list">{w.instances.map((i:any)=>instanceRow(w.id+':'+i.id,'#'+i.id.split('~')[0],i.users,w.capacity))}</div>:<p className="fineprint">{tr("VRChat lists no open public instances right now.")}</p>}
    <div className="instance-new"><select aria-label={tr("Region")} value={region} onChange={e=>setRegion(e.target.value)}>{Object.entries(regionNames).map(([k,v])=><option key={k} value={k}>{v}</option>)}</select><button className="primary" disabled={!!busy} onClick={startInstance}>{busy.startsWith('newInstance')?<LoaderCircle size={14} className="spin"/>:<Play size={14}/>}{tr("New public instance")}</button></div>
    <p className="fineprint">{tr("“Invite me” sends you an in-game invite. “Launch” opens the instance in VRChat on this device.")}</p></div><div className="detail-actions"><AddToCollection kind="world" target={w.id} name={w.name||''} image={w.thumbnailImageUrl||w.imageUrl} className=""/></div></>;
  extras=<Section title={'More by '+w.authorName} state={{...userWorlds,data:userWorlds.data?.filter((x:any)=>x.id!==w.id)}}><div className="entity-grid">{userWorlds.data?.filter((x:any)=>x.id!==w.id).map((x:any)=><a className="entity-card" key={x.id} {...link('world',x.id)}><div className="entity-card-image wide"><Photo url={x.thumbnailImageUrl} name={x.name}/></div><strong>{x.name}</strong><small><Users size={11}/>{count(x.occupants)} {tr("online")}</small></a>)}</div></Section>;
 }else if(kind==='user'){const u=data;
  image=<Photo url={userImage(u)} name={u.displayName} alt=""/>;
  eyebrow=<><UserRound size={13}/>{trustRank(u.tags).toUpperCase()}{u.isFriend?' / FRIEND':''}</>;
  title=u.displayName;subtitle=<>{u.pronouns&&<span className="user-pronouns">{u.pronouns}</span>}<UserStatus user={u}/></>;
  body=<><p className="description">{u.bio||tr("No bio yet.")}</p>{safeLinks(u.bioLinks).length>0&&<div className="bio-links">{safeLinks(u.bioLinks).map(l=><a key={l} className="text-link" href={l} target="_blank" rel="noreferrer nofollow">{l.replace(/^https:\/\/(www\.)?/,'').slice(0,60)}<ExternalLink size={12}/></a>)}</div>}</>;
  meta=<>{u.location&&<span>{tr("Location")}<strong>{u.location==='private'?'In a private instance':u.location==='offline'?tr("Offline"):u.location==='traveling'?'Traveling':place.data?<a className="text-link" {...link('world',place.data.id)}><MapPin size={12}/>{place.data.name}</a>:'In a world'}</strong></span>}<span>{tr("Joined VRChat")}<strong>{date(u.dateJoined)}</strong></span>{u.lastLogin&&<span>{tr("Last login")}<strong>{date(u.lastLogin)}</strong></span>}{u.lastPlatform&&<span>{tr("Last platform")}<strong>{platformName(u.lastPlatform)}</strong></span>}</>;
  actions=<>{u.badges?.length>0&&<div className="badge-row">{u.badges.map((b:any)=><span key={b.id||b.name} title={b.description||b.name}><Photo url={b.imageUrl} name={b.name} alt={b.name}/></span>)}</div>}
   {u.location?.startsWith('wrld_')&&<div className="detail-section"><h3>{tr("Join")} {u.displayName}</h3><div className="instance-list">{instanceRow(u.location,place.data?.name||'Their instance',undefined,place.data?.capacity)}</div></div>}
   {u.id!==user?.id&&<div className="detail-actions">{u.isFriend?<><button disabled={!!busy} onClick={()=>act('requestInvite',u.id,'Invite request sent.')}><Send size={16}/>{tr("Request invite")}</button><button className="danger-text" disabled={!!busy} onClick={()=>setConfirm({action:'unfriend',id:u.id,text:'Remove '+u.displayName+' from your friends?',done:'Friend removed.',patch:{isFriend:false}})}><UserMinus size={16}/>{tr("Unfriend")}</button></>:<button className="primary" disabled={!!busy} onClick={()=>setConfirm({action:'friend',id:u.id,text:'Send a friend request to '+u.displayName+'?',done:'Friend request sent.'})}><UserPlus size={16}/>{tr("Add friend")}</button>}</div>}</>;
  extras=<><Section title={tr("Worlds")} state={userWorlds}><div className="entity-grid">{userWorlds.data?.map((x:any)=><a className="entity-card" key={x.id} {...link('world',x.id)}><div className="entity-card-image wide"><Photo url={x.thumbnailImageUrl} name={x.name}/></div><strong>{x.name}</strong><small><Users size={11}/>{count(x.occupants)} {tr("online")}</small></a>)}</div></Section>
   <Section title={tr("Groups")} state={userGroups} empty="No public group memberships."><div className="entity-grid">{userGroups.data?.map((g:any)=><a className="entity-card" key={g.id} {...link('group',g.id)}><div className="entity-card-image"><Photo url={g.iconUrl} fallbackUrl={g.bannerUrl} name={g.name}/></div><strong>{g.name}{g.isRepresenting&&<BadgeCheck size={13}/>}</strong><small>{g.shortCode}.{g.discriminator} · {count(g.memberCount)} {tr("members")}</small></a>)}</div></Section></>;
 }else{const g=data;const status=g.membershipStatus;
  image=<Photo url={g.bannerUrl} fallbackUrl={g.iconUrl} name={g.name} alt=""/>;
  eyebrow=<><Users size={13}/>{tr("GROUP")}{g.isVerified?' / VERIFIED':''}{g.privacy==='private'?' / PRIVATE':''}</>;
  title=<span className="group-title">{g.iconUrl&&<span className="group-icon"><Photo url={g.iconUrl} fallbackUrl={g.bannerUrl} name={g.name}/></span>}{g.name}</span>;
  subtitle=<>{g.shortCode}.{g.discriminator}{owner.data&&<> {tr("· owned by")} <a {...link('user',owner.data.id)}>{owner.data.displayName}</a></>}</>;
  body=<><p className="description">{g.description||tr("No description.")}</p>{safeLinks(g.links).length>0&&<div className="bio-links">{safeLinks(g.links).map(l=><a key={l} className="text-link" href={l} target="_blank" rel="noreferrer nofollow">{l.replace(/^https:\/\/(www\.)?/,'').slice(0,60)}<ExternalLink size={12}/></a>)}</div>}</>;
  meta=<><span>{tr("Members")}<strong>{count(g.memberCount)}{typeof g.onlineMemberCount==='number'?' ('+count(g.onlineMemberCount)+' online)':''}</strong></span><span>{tr("Joining")}<strong>{joinStates[g.joinState]||g.joinState||'-'}</strong></span><span>{tr("Your membership")}<strong>{status==='member'?'Member':status==='requested'?'Request pending':status==='invited'?'Invited':status==='banned'?'Banned':'Not a member'}</strong></span>{g.languages?.length>0&&<span>{tr("Languages")}<strong>{g.languages.join(', ')}</strong></span>}<span>{tr("Created")}<strong>{date(g.createdAt)}</strong></span></>;
  const tags=(g.tags||[]).filter((t:string)=>!t.startsWith('admin_')&&!t.startsWith('system_'));
  actions=<>{tags.length>0&&<div className="tags">{tags.map((t:string)=><span key={t}>{t.replaceAll('_',' ')}</span>)}</div>}
   {g.rules&&<div className="detail-section"><h3>{tr("Rules")}</h3><p className="description rules">{g.rules}</p></div>}
   <div className="detail-actions">{status==='member'?<button className="danger-text" disabled={!!busy} onClick={()=>setConfirm({action:'leaveGroup',id:g.id,text:'Leave '+g.name+'?',done:'You left the group.',patch:{membershipStatus:'inactive'}})}><LogOut size={16}/>{tr("Leave group")}</button>:status==='requested'?<button disabled><LoaderCircle size={16}/>{tr("Join request pending")}</button>:<button className="primary" disabled={!!busy||g.joinState==='closed'||(g.joinState==='invite'&&status!=='invited')} onClick={()=>setConfirm({action:'joinGroup',id:g.id,text:(g.joinState==='request'?'Request to join ':'Join ')+g.name+'? VRChat applies the group’s access rules.',done:g.joinState==='request'?'Join request sent.':'Joined group.',patch:{membershipStatus:g.joinState==='request'?'requested':'member'}})}><LogIn size={16}/>{g.joinState==='request'?'Request to join':g.joinState==='invite'&&status!=='invited'?'Invite only':g.joinState==='closed'?'Closed':'Join group'}</button>}</div></>;
  extras=<>{status==='member'&&<Section title={tr("Group instances")} state={groupInstances} empty="No group instances are open right now."><div className="instance-list">{groupInstances.data?.map((i:any)=>instanceRow(i.location,i.worldName||'#'+i.location.split(':')[1].split('~')[0],i.users,i.capacity))}</div></Section>}
   <Section title={tr("Posts")} state={posts} empty="No posts you can see."><div className="post-list">{posts.data?.map((p:any)=><article key={p.id}>{p.imageUrl&&<div className="post-image"><Photo url={p.imageUrl} alt=""/></div>}<div><h4>{p.title}</h4><small>{date(p.createdAt)}</small><p>{p.text}</p></div></article>)}</div></Section>
   <Section title={tr("Members")} state={members} empty="VRChat doesn’t share this group’s member list with you."><div className="entity-grid people">{members.data?.map((m:any)=><a className="entity-card" key={m.id} {...link('user',m.id)}><div className="entity-card-image"><Photo url={userThumb(m)} name={m.displayName}/></div><strong>{m.displayName}</strong><small>{tr("Joined")} {date(m.joinedAt)}</small></a>)}</div></Section></>;
 }
 if(!page)return <div className={'detail detail-'+kind}><div className="detail-image">{image}</div><div className="detail-copy">{toolbar}<div className="eyebrow">{eyebrow}</div><h2>{title}</h2><p className="creator">{subtitle}</p>{body}<div className="detail-meta">{meta}</div>{actions}{confirmBox}<Reviews kind={kind} id={id} name={typeof data.name==='string'?data.name:data.displayName}/>{idRow}<RawJson kind={kind} id={id} user={user} notify={notify}/></div></div>;
 return <div className="entity-page">{toolbar}<div className={'entity-hero '+kind}><div className="entity-hero-image">{image}</div><div className="entity-hero-copy"><div className="eyebrow">{eyebrow}</div><h1>{title}</h1><p className="creator">{subtitle}</p></div></div>
  <div className="entity-layout"><div className="entity-main">{body}{actions}{confirmBox}{extras}<Reviews kind={kind} id={id} name={typeof data.name==='string'?data.name:data.displayName}/><RawJson kind={kind} id={id} user={user} notify={notify}/></div><aside className="entity-aside"><div className="detail-meta">{meta}</div>{idRow}<a className="text-link" href={'https://vrchat.com/home/'+kind+'/'+id} target="_blank" rel="noreferrer">{tr("Open on vrchat.com")}<ExternalLink size={12}/></a></aside></div></div>;
}
export default function DetailHost({coveAccount,user,onLogin,notify}:any){useLocale();
 const [stack,setStack]=useState<Target[]>([]);const ref=useRef<HTMLDivElement>(null);const target=stack.at(-1);
 useEffect(()=>{const open=(e:Event)=>{const t=(e as CustomEvent<Target>).detail;setStack(s=>s.at(-1)?.id===t.id?s:[...s,t].slice(-10));};const close=()=>setStack([]);window.addEventListener('vrcove:detail',open);window.addEventListener('hashchange',close);return()=>{window.removeEventListener('vrcove:detail',open);window.removeEventListener('hashchange',close);};},[]);
 useEffect(()=>{if(!target)return;ref.current?.focus();const old=document.body.style.overflow;document.body.style.overflow='hidden';return()=>{document.body.style.overflow=old;};},[target?.id]);
 if(!target)return null;
 const close=()=>setStack([]);
 return <div className="modal-backdrop" onMouseDown={e=>{if(e.target===e.currentTarget)close();}}><div className="modal wide" role="dialog" aria-modal="true" aria-label={target.kind+' details'} tabIndex={-1} ref={ref} onKeyDown={e=>{if(e.key==='Escape'){e.stopPropagation();close();}}}>
  <button className="modal-close" onClick={close} aria-label={tr("Close")}>×</button>
  <DetailView key={target.kind+target.id} coveAccount={coveAccount} kind={target.kind} id={target.id} user={user} onLogin={()=>{close();onLogin();}} notify={notify} mode="modal" onBack={stack.length>1?()=>setStack(s=>s.slice(0,-1)):undefined} onClose={close}/>
 </div></div>;
}
