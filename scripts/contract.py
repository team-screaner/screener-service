import json
from pathlib import Path
import os
os.chdir(Path(__file__).resolve().parent.parent)
string={'type':'string'}
def obj(props,required=[]): return {'type':'object','properties':props,'required':required,'additionalProperties':False}
def arr(v):return {'type':'array','items':v}
def ref(s):return {'$ref':'#/components/schemas/'+s}
S={
'Error':obj({'code':string,'message':string},['code','message']),
'Credentials':obj({'email':{'type':'string','format':'email'},'password':{'type':'string','minLength':12,'maxLength':72},'name':string},['email','password']),
'RegisterInput':obj({'email':{'type':'string','format':'email'},'password':{'type':'string','minLength':12,'maxLength':72},'name':string},['email','password','name']),
'TokenRequest':obj({'name':string,'scopes':arr({'type':'string','enum':['matrix:read','evidence:read','evidence:write','assessment:read']}),'expires_in_days':{'type':'integer','minimum':1,'maximum':90}},['name','scopes']),
'SkillInput':obj({'key':string,'name':string,'description':string,'category':string,'scope':{'type':'string','enum':['personal','organization']},'organization_id':string,'signals':arr(string)},['key','name']),
'Level':obj({'id':string,'key':string,'name':string,'description':string,'order':{'type':'integer'}},['key','name']),
'Group':obj({'id':string,'key':string,'name':string,'weight':{'type':'number','exclusiveMinimum':True,'minimum':0}},['key','name','weight']),
'MatrixSkill':obj({'skill_id':string,'group_key':string,'weight':{'type':'number','minimum':0,'exclusiveMinimum':True},'required':{'type':'boolean'},'active':{'type':'boolean'},'order':{'type':'integer'}},['skill_id','group_key','weight']),
'Requirement':obj({'id':string,'skill_id':string,'level_key':string,'description':string,'required':{'type':'boolean'},'critical':{'type':'boolean'},'weight':{'type':'number','minimum':0,'exclusiveMinimum':True}},['skill_id','level_key','description','weight']),
'MatrixInput':obj({'name':string,'description':string,'scope':{'type':'string','enum':['personal','organization']},'organization_id':string,'visibility':{'type':'string','enum':['private','organization','public']},'levels':arr(ref('Level')),'groups':arr(ref('Group')),'skills':arr(ref('MatrixSkill')),'requirements':arr(ref('Requirement'))},['name','levels','groups','skills','requirements']),
'UserMatrixInput':obj({'matrix_version_id':string,'current_level_id':string,'target_level_id':string},['matrix_version_id','current_level_id','target_level_id']),
'Match':obj({'skill_key':string,'requirement_id':string,'confidence':{'type':'number','minimum':0,'maximum':1},'reason':string},['skill_key','confidence','reason']),
'EvidenceInput':obj({'external_id':string,'title':string,'description':string,'fact_type':string,'period_from':{'type':'string','format':'date'},'period_to':{'type':'string','format':'date'},'source':{'type':'string','enum':['manual','github','gitlab','jira','confluence','linear','ai_import','api','other']},'source_url':string,'matches':arr(ref('Match'))},['title','description','fact_type','source','matches']),
'EvidenceBatch':obj({'period':string,'items':{'type':'array','minItems':1,'maxItems':100,'items':ref('EvidenceInput')}},['items']),
'ReviewInput':obj({'status':{'type':'string','enum':['accepted','rejected']},'title':string,'description':string,'version':{'type':'integer'}},['status','version']),
'AssessmentItem':obj({'requirement_id':string,'score':{'type':'number','minimum':0,'maximum':4},'status':{'type':'string','enum':['assessed','not_applicable']},'comment':string,'na_reason':string,'evidence_ids':arr(string)},['requirement_id','score','status']),
'AssessmentInput':obj({'user_matrix_id':string,'period':string,'type':{'type':'string','enum':['self','manager']},'reviewed_assessment_id':string,'items':arr(ref('AssessmentItem'))},['user_matrix_id','period','type','items']),
'ReviewerInput':obj({'email':{'type':'string','format':'email'}},['email']),
'OverrideInput':obj({'user_matrix_id':string,'requirement_id':string,'description':string,'reason':string},['user_matrix_id','requirement_id','description','reason']),
'GrowthItem':obj({'id':string,'requirement_id':string,'action':string,'status':{'type':'string','enum':['planned','in_progress','done']},'evidence_ids':arr(string)},['requirement_id','action','status']),
'GrowthInput':obj({'user_matrix_id':string,'deadline':{'type':'string','format':'date'},'items':arr(ref('GrowthItem'))},['user_matrix_id','deadline','items']),
'OrganizationInput':obj({'name':string},['name']),
'MemberInput':obj({'user_id':string,'role':{'type':'string','enum':['admin','manager','member']}},['user_id','role']),
'CopyInput':obj({'name':string},['name']),
'VersionUpdate':obj({'expected_version':{'type':'integer'},'matrix':ref('MatrixInput')},['expected_version','matrix']),
'UpstreamInput':obj({'action':{'type':'string','enum':['apply','ignore']},'upstream_version_id':string},['action','upstream_version_id']),
'ImportInput':obj({'file_base64':string,'sheet':string,'mapping':obj({'category':string,'skill':string,'levels':arr(string)}),'name':string,'confirm':{'type':'boolean'}},['file_base64']),
'Object':{'type':'object','additionalProperties':True},
}
paths={}
def route(path,method,op,body=None,public=False,params=[]):
 p=[]
 import re
 for name in re.findall(r'{(\w+)}',path):p.append({'name':name,'in':'path','required':True,'schema':string})
 for name in params:p.append({'name':name,'in':'query','schema':string})
 if method in ('post','put','patch','delete') and op not in ('login','logout'):
  p.append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':200}})
 d={'operationId':op,'responses':{'200':{'description':'Success; list responses contain items and next_cursor.','content':{'application/json':{'schema':ref('Object')}}},**{str(c):{'description':msg,'content':{'application/json':{'schema':ref('Error')}}}for c,msg in [(400,'Invalid input'),(401,'Authentication required'),(403,'Forbidden'),(404,'Not found'),(409,'State or version conflict'),(422,'Idempotency key reused with different payload'),(500,'Internal error')]} }}
 if p:d['parameters']=p
 if public:d['security']=[]
 if body:d['requestBody']={'required':True,'content':{'application/json':{'schema':ref(body)}}}
 paths.setdefault(path,{})[method]=d
routes=[('/auth/register','post','register','RegisterInput',True),('/auth/login','post','login','Credentials',True),('/auth/logout','post','logout'),('/me','get','getMe'),('/tokens','post','createToken','TokenRequest'),('/tokens','get','listTokens'),('/tokens/{id}','delete','revokeToken'),('/organizations','post','createOrganization','OrganizationInput'),('/organizations','get','listOrganizations'),('/organizations/{id}/members','post','addMember','MemberInput'),('/organizations/{id}/members','get','listMembers'),('/skills','post','createSkill','SkillInput'),('/skills','get','listSkills'),('/skills/{id}','patch','deactivateSkill','Object'),('/fact-types','get','listFactTypes'),('/matrices','post','createMatrix','MatrixInput'),('/matrices','get','listMatrices'),('/matrices/{id}','get','getMatrix'),('/matrices/{id}/versions','post','createVersion','MatrixInput'),('/matrices/{id}/versions','get','listVersions'),('/matrix-versions/{id}','get','getVersion'),('/matrix-versions/{id}','put','updateVersion','VersionUpdate'),('/matrix-versions/{id}/publish','post','publishVersion'),('/matrices/{id}/fork','post','forkMatrix','CopyInput'),('/matrices/{id}/duplicate','post','duplicateMatrix','CopyInput'),('/matrices/{id}/upstream','get','getUpstream'),('/matrices/{id}/upstream','post','reviewUpstream','UpstreamInput'),('/user-matrices','post','createUserMatrix','UserMatrixInput'),('/user-matrices','get','listUserMatrices'),('/overrides','post','setOverride','OverrideInput'),('/evidence','post','createEvidence','EvidenceInput'),('/evidence','get','listEvidence'),('/evidence/batch','post','batchEvidence','EvidenceBatch'),('/evidence/{id}','patch','reviewEvidence','ReviewInput'),('/assessments','post','createAssessment','AssessmentInput'),('/assessments','get','listAssessments'),('/assessments/{id}','get','getAssessment'),('/me/growth-context','get','getGrowthContext'),('/me/gaps','get','getGaps'),('/me/radar','get','getRadar'),('/growth-plans','post','createGrowthPlan','GrowthInput'),('/growth-plans','get','listGrowthPlans'),('/growth-plans/{id}','put','updateGrowthPlan','GrowthInput'),('/import/xlsx','post','importXLSX','ImportInput'),('/export/xlsx','get','exportXLSX'),('/audit','get','listAudit')]
routes += [('/user-matrices/{id}/reviewer','get','getReviewer'),('/user-matrices/{id}/reviewer','put','setReviewer','ReviewerInput'),('/user-matrices/{id}/reviewer','delete','removeReviewer'),('/reviews','get','listReviews'),('/reviews/{id}','get','getReview')]
for row in routes:
 path,method,op,*rest=row
 body=rest[0] if rest else None;public=rest[1] if len(rest)>1 else False
 params=['limit','cursor'] if method=='get' else []
 if path in ['/me/growth-context','/me/gaps','/me/radar','/assessments','/growth-plans','/export/xlsx'] and method=='get': params+=['user_matrix_id']
 if path=='/export/xlsx':params+=['matrix_version_id']
 route('/api/v1'+path,method,op,body,public,params)
# Response objects retain additional metadata for forward-compatible clients.
def output(props,required=[]):
 value=obj(props,required);value['additionalProperties']=True;return value
S.update({
 'User':output({'id':string,'email':string,'name':string},['id','email','name']),
 'TokenResult':output({'id':string,'user_id':string,'token':string,'expires_at':{'type':'string','format':'date-time'},'scopes':arr(string)},['id','user_id','token','expires_at']),
 'Skill':output({'id':string,'key':string,'name':string,'description':string,'category':string,'scope':string,'active':{'type':'boolean'},'signals':arr(string)},['id','key','name']),
 'FactType':output({'key':string,'name':string},['key','name']),
 'VersionLevel':output({'id':string,'key':string,'name':string,'order':{'type':'integer'}},['id','key','name','order']),
 'VersionSkill':output({'id':string,'skill_id':string,'skill_key':string,'name':string,'group_id':string,'weight':{'type':'number'},'active':{'type':'boolean'},'required':{'type':'boolean'},'signals':arr(string)},['id','skill_id','skill_key','group_id']),
 'VersionRequirement':output({'id':string,'skill_id':string,'level_id':string,'level_key':string,'description':string,'required':{'type':'boolean'},'critical':{'type':'boolean'},'weight':{'type':'number'}},['id','skill_id','level_id','description']),
 'MatrixVersion':output({'id':string,'matrix_id':string,'number':{'type':'integer'},'status':string,'version':{'type':'integer'},'levels':arr(ref('VersionLevel')),'groups':arr(ref('Object')),'skills':arr(ref('VersionSkill')),'requirements':arr(ref('VersionRequirement'))},['id','matrix_id','number','status']),
 'Matrix':output({'id':string,'name':string,'description':string,'scope':string,'visibility':string,'status':string,'versions':arr(ref('MatrixVersion')),'version':ref('MatrixVersion')},['id','name']),
 'UserMatrix':output({'id':string,'user_id':string,'matrix_version_id':string,'current_level_id':string,'target_level_id':string,'status':string},['id','user_id','matrix_version_id','current_level_id','target_level_id']),
 'EvidenceMatch':output({**S['Match']['properties'],'id':string,'requirement_id':{'type':'string','nullable':True}},['id','skill_key','confidence','reason']),
 'Evidence':output({'id':string,'user_id':string,'title':string,'description':string,'fact_type':string,'source':string,'status':string,'version':{'type':'integer'},'matches':arr(ref('EvidenceMatch'))},['id','title','status','version','matches']),
 'AssessmentSnapshotItem':output({**S['AssessmentItem']['properties'],'requirement_snapshot':string},['requirement_id','score','status','requirement_snapshot']),
 'Assessment':output({'id':string,'user_id':string,'user_matrix_id':string,'matrix_version_id':string,'period':string,'type':string,'items':arr(ref('AssessmentSnapshotItem'))},['id','user_matrix_id','matrix_version_id','period','type']),
 'Readiness':output({'percent':{'type':'number','minimum':0,'maximum':100},'ready':{'type':'boolean'},'critical_gaps':arr(string),'gaps':arr(ref('Object')),'groups':arr(ref('Object')),'user_matrix_id':string,'policy':string},['percent','ready','critical_gaps','gaps','groups','user_matrix_id']),
 'GrowthContext':output({'user':ref('User'),'user_matrix':ref('UserMatrix'),'matrix':ref('Matrix'),'matrix_version':ref('MatrixVersion'),'current_level':ref('VersionLevel'),'target_level':ref('VersionLevel'),'competencies':arr(ref('Object')),'requirements':arr(ref('VersionRequirement')),'skills':arr(ref('VersionSkill')),'signals':arr(ref('Object')),'fact_types':arr(ref('FactType')),'evidence':arr(ref('Evidence'))},['user','user_matrix','matrix','matrix_version','requirements','skills','fact_types','evidence']),
 'GrowthPlan':output({'id':string,'user_matrix_id':string,'target_level_id':string,'deadline':string,'items':arr(ref('GrowthItem'))},['id','user_matrix_id','target_level_id','deadline','items']),
})
S['Evidence']['properties'].update({'external_id':{'type':'string','nullable':True},'source_url':string,'period_from':{'type':'string','nullable':True},'period_to':{'type':'string','nullable':True},'created_at':string,'updated_at':string})
S['Matrix']['properties'].update({'parent_matrix_id':{'type':'string','nullable':True},'parent_version_id':{'type':'string','nullable':True},'owner_id':{'type':'string','nullable':True},'created_at':string})
S['UserMatrix']['properties']['created_at']=string
S['Assessment']['properties'].update({'created_at':string,'assessor_id':{'type':'string','nullable':True},'reviewed_assessment_id':{'type':'string','nullable':True}})
S['Readiness']['properties']['assessment_id']={'type':'string','nullable':True}
S['RadarRequirementGroup']=output({'group_id':string,'name':string,'requirements_count':{'type':'integer'},'expected_score':{'type':'number'}},['group_id','name','requirements_count','expected_score'])
S['RadarScoreGroup']=output({'group_id':string,'name':string,'percent':{'type':'number'}},['group_id','name','percent'])
S['RadarSeries']=output({'self':arr(ref('RadarScoreGroup')),'manager':{'type':'array','items':ref('RadarScoreGroup'),'nullable':True},'current_requirements':arr(ref('RadarRequirementGroup')),'target_requirements':arr(ref('RadarRequirementGroup'))},['self','manager','current_requirements','target_requirements'])
S['Radar']=output({**S['Readiness']['properties'],'series':ref('RadarSeries')},S['Readiness']['required']+['series'])
S['Radar']['properties'].update({'manager_assessment_id':{'type':'string','nullable':True},'period':{'type':'string','nullable':True},'manager_author':{'allOf':[ref('User')],'nullable':True}})
S['Reviewer']=output({'user_matrix_id':string,'reviewer':{'allOf':[ref('User')],'nullable':True}},['user_matrix_id','reviewer'])
S['ReviewAssignment']=output({'id':string,'user':ref('User'),'matrix_name':string,'target_level_name':string,'assessment_id':{'type':'string','nullable':True}},['id','user','matrix_name','target_level_name','assessment_id'])
S['ReviewContext']=output({'context':ref('GrowthContext'),'assessment':{'allOf':[ref('Assessment')],'nullable':True},'manager_assessment':{'allOf':[ref('Assessment')],'nullable':True}},['context','assessment','manager_assessment'])
response_types={'getReviewer':'Reviewer','setReviewer':'Reviewer','removeReviewer':'Reviewer','getReview':'ReviewContext','register':'TokenResult','login':'TokenResult','createToken':'TokenResult','getMe':'User','createSkill':'Skill','deactivateSkill':'Skill','createMatrix':'Matrix','getMatrix':'Matrix','forkMatrix':'Matrix','duplicateMatrix':'Matrix','createVersion':'MatrixVersion','getVersion':'MatrixVersion','updateVersion':'MatrixVersion','publishVersion':'MatrixVersion','createUserMatrix':'UserMatrix','createEvidence':'Evidence','reviewEvidence':'Evidence','createAssessment':'Assessment','getAssessment':'Assessment','getGrowthContext':'GrowthContext','getGaps':'Readiness','getRadar':'Radar','createGrowthPlan':'GrowthPlan','updateGrowthPlan':'GrowthPlan'}
list_types={'listReviews':'ReviewAssignment','listSkills':'Skill','listFactTypes':'FactType','listMatrices':'Matrix','listVersions':'MatrixVersion','listUserMatrices':'UserMatrix','listEvidence':'Evidence','batchEvidence':'Evidence','listAssessments':'Assessment','listGrowthPlans':'GrowthPlan'}
for op,typ in list_types.items():
 name=typ+'Page';S[name]=output({'items':arr(ref(typ)),'next_cursor':string},['items']);response_types[op]=name
for path,ops in paths.items():
 for method,operation in ops.items():
  op=operation['operationId']
  if op in response_types:operation['responses']['200']['content']['application/json']['schema']=ref(response_types[op])
  if op=='exportXLSX':operation['responses']['200']={'description':'Portable workbook; user_matrix_id includes Assessment and Evidence sheets.','content':{'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet':{'schema':{'type':'string','format':'binary'}}}}
  if op=='listSkills':
   operation['parameters'] += [{'name':name,'in':'query','schema':string} for name in ['category','scope','active']]
spec={'openapi':'3.0.3','info':{'title':'Screener API','version':'0.1.0','description':'Bearer sessions and scoped agent tokens. Score 0..4. Cursor lists are keyset ordered by stable ID and bound to user/path/filters. Every command except login/logout requires Idempotency-Key; identical canonical JSON replays within same principal+method+path, changed payload returns 422. Evidence acceptance never changes assessment. XLSX export returns application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.'},'security':[{'bearerAuth':[]}],'paths':paths,'components':{'securitySchemes':{'bearerAuth':{'type':'http','scheme':'bearer'}},'schemas':S}}
Path('api/openapi.yaml').write_text(json.dumps(spec,indent=2)+'\n')
