# Evidence and XLSX export query plans

Measured locally on 2026-09-23 using PostgreSQL 17 (`postgres:17-alpine`, Docker Desktop on ARM64). These are actual `EXPLAIN (ANALYZE, BUFFERS)` results, not production latency guarantees. Each query was run once after `ANALYZE`; most pages were already cached by fixture insertion. No claim is made that all service workloads are tuned.

Fixture: 20,000 evidence records and 20,000 matches, split equally between two users; 1,000 skills; one assigned published matrix with 500 skills, 10 levels, 5,000 requirements and 100 personal overrides; latest assessment with 500 items. IDs are deterministic UUID hashes. This exercises selective owner/identity lookups and substantial exports, not skew across thousands of organizations.

The evidence export query deliberately returns at most 2,001 rows: the bridge rejects more than 2,000 supplemental rows rather than silently truncating a workbook. Its plan below measures the overflow-detection query, not a successful full 10,000-row export.

| Query | Execution time |
|---|---|
| Evidence owner page | 1.263 ms |
| External import identity | 0.118 ms |
| Assigned requirement match | 0.153 ms |
| Export matrix rows | 1.154 ms |
| Export effective requirements | 7.121 ms |
| Export latest assessment | 3.350 ms |
| Export owned evidence | 18.086 ms |

The owner page uses the `(user_id,id)` index and bounded per-item match lookups. External identity uses the unique `(user_id,source,external_id)` index. Requirement matching starts from the requirement primary key and checks published assignment. Exports scan a large proportion of their fixture tables, so sequential/hash plans are expected. Re-measure with production distributions and concurrent writes before changing indexes.

## Evidence owner page

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT jsonb_build_object(
 'id',e.id,'user_id',e.user_id,'title',e.title,'description',e.description,
 'fact_type',e.fact_type,'period_from',e.period_from,'period_to',e.period_to,
 'source',e.source,'source_url',e.source_url,'external_id',e.external_id,
 'status',e.status,'version',e.revision,'created_at',e.created_at,'updated_at',e.updated_at,
 'matches',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',em.id,'skill_key',s.key,
 'requirement_id',em.requirement_id,'confidence',em.confidence,'reason',em.reason) ORDER BY em.id)
 FROM evidence_matches em JOIN skills s ON s.id=em.skill_id WHERE em.evidence_id=e.id),'[]'::jsonb)) FROM evidence e WHERE e.user_id='00000000-0000-0000-0000-000000000001' AND e.id>'00000000-0000-0000-0000-000000000000' ORDER BY e.id LIMIT 51;
```

```text
Limit  (cost=0.29..864.18 rows=51 width=48) (actual time=0.112..1.171 rows=51 loops=1)
  Buffers: shared hit=362
  ->  Index Scan using evidence_user on evidence e  (cost=0.29..169390.23 rows=10000 width=48) (actual time=0.111..1.166 rows=51 loops=1)
        Index Cond: ((user_id = '00000000-0000-0000-0000-000000000001'::uuid) AND (id > '00000000-0000-0000-0000-000000000000'::uuid))
        Buffers: shared hit=362
        SubPlan 1
          ->  Aggregate  (cost=16.64..16.65 rows=1 width=32) (actual time=0.014..0.014 rows=1 loops=51)
                Buffers: shared hit=309
                ->  Sort  (cost=16.62..16.63 rows=1 width=64) (actual time=0.008..0.008 rows=1 loops=51)
                      Sort Key: em.id
                      Sort Method: quicksort  Memory: 25kB
                      Buffers: shared hit=309
                      ->  Nested Loop  (cost=0.56..16.61 rows=1 width=64) (actual time=0.004..0.005 rows=1 loops=51)
                            Buffers: shared hit=306
                            ->  Index Scan using evidence_matches_evidence_id_skill_id_requirement_id_key on evidence_matches em  (cost=0.29..8.30 rows=1 width=71) (actual time=0.003..0.003 rows=1 loops=51)
                                  Index Cond: (evidence_id = e.id)
                                  Buffers: shared hit=153
                            ->  Index Scan using skills_pkey on skills s  (cost=0.28..8.29 rows=1 width=25) (actual time=0.001..0.001 rows=1 loops=51)
                                  Index Cond: (id = em.skill_id)
                                  Buffers: shared hit=153
Planning:
  Buffers: shared hit=314 read=1
Planning Time: 1.287 ms
Execution Time: 1.263 ms
```

## External import identity

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT id,import_hash FROM evidence WHERE user_id='00000000-0000-0000-0000-000000000001' AND source='github' AND external_id='external-501' FOR UPDATE;
```

```text
LockRows  (cost=0.41..8.44 rows=1 width=87) (actual time=0.072..0.073 rows=1 loops=1)
  Buffers: shared hit=6
  ->  Index Scan using evidence_user_id_source_external_id_key on evidence  (cost=0.41..8.43 rows=1 width=87) (actual time=0.038..0.039 rows=1 loops=1)
        Index Cond: ((user_id = '00000000-0000-0000-0000-000000000001'::uuid) AND (source = 'github'::text) AND (external_id = 'external-501'::text))
        Buffers: shared hit=4
Planning:
  Buffers: shared hit=122
Planning Time: 0.487 ms
Execution Time: 0.118 ms
```

## Assigned requirement match

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT s.id FROM requirements r JOIN matrix_skills ms ON ms.id=r.matrix_skill_id JOIN skills s ON s.id=ms.skill_id JOIN matrix_versions mv ON mv.id=r.matrix_version_id WHERE r.id=md5('req-1-1')::uuid AND s.key='skill.1' AND s.active AND (s.scope='system' OR (s.scope='personal' AND s.owner_id='00000000-0000-0000-0000-000000000001') OR (s.scope='organization' AND EXISTS(SELECT 1 FROM organization_members om WHERE om.organization_id=s.owner_id AND om.user_id='00000000-0000-0000-0000-000000000001'))) AND mv.status='published' AND ms.active AND EXISTS(SELECT 1 FROM user_matrices um WHERE um.user_id='00000000-0000-0000-0000-000000000001' AND um.matrix_version_id=r.matrix_version_id AND um.status='active');
```

```text
Nested Loop  (cost=0.83..19.36 rows=1 width=16) (actual time=0.060..0.061 rows=1 loops=1)
  Join Filter: (mv.id = um.matrix_version_id)
  Buffers: shared hit=11
  ->  Nested Loop  (cost=0.83..18.33 rows=1 width=48) (actual time=0.051..0.052 rows=1 loops=1)
        Join Filter: (r.matrix_version_id = mv.id)
        Buffers: shared hit=10
        ->  Nested Loop  (cost=0.83..17.31 rows=1 width=32) (actual time=0.044..0.045 rows=1 loops=1)
              Buffers: shared hit=9
              ->  Nested Loop  (cost=0.55..16.62 rows=1 width=32) (actual time=0.029..0.029 rows=1 loops=1)
                    Buffers: shared hit=6
                    ->  Index Scan using requirements_pkey on requirements r  (cost=0.28..8.30 rows=1 width=32) (actual time=0.014..0.014 rows=1 loops=1)
                          Index Cond: (id = 'a9abe32c-fc8e-6c15-5a6e-ff0927b7e10f'::uuid)
                          Buffers: shared hit=3
                    ->  Index Scan using matrix_skills_pkey on matrix_skills ms  (cost=0.27..8.29 rows=1 width=32) (actual time=0.013..0.013 rows=1 loops=1)
                          Index Cond: (id = r.matrix_skill_id)
                          Filter: active
                          Buffers: shared hit=3
              ->  Index Scan using skills_pkey on skills s  (cost=0.28..0.48 rows=1 width=16) (actual time=0.015..0.015 rows=1 loops=1)
                    Index Cond: (id = ms.skill_id)
                    Filter: (active AND (key = 'skill.1'::text) AND ((scope = 'system'::text) OR ((scope = 'personal'::text) AND (owner_id = '00000000-0000-0000-0000-000000000001'::uuid)) OR ((scope = 'organization'::text) AND EXISTS(SubPlan 1))))
                    Buffers: shared hit=3
                    SubPlan 1
                      ->  Seq Scan on organization_members om  (cost=0.00..0.00 rows=1 width=0) (never executed)
                            Filter: ((organization_id = s.owner_id) AND (user_id = '00000000-0000-0000-0000-000000000001'::uuid))
        ->  Seq Scan on matrix_versions mv  (cost=0.00..1.01 rows=1 width=16) (actual time=0.006..0.006 rows=1 loops=1)
              Filter: (status = 'published'::text)
              Buffers: shared hit=1
  ->  Seq Scan on user_matrices um  (cost=0.00..1.01 rows=1 width=16) (actual time=0.008..0.008 rows=1 loops=1)
        Filter: ((user_id = '00000000-0000-0000-0000-000000000001'::uuid) AND (status = 'active'::text))
        Buffers: shared hit=1
Planning:
  Buffers: shared hit=530 read=2
Planning Time: 2.491 ms
Execution Time: 0.153 ms
```

## Export matrix rows

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT jsonb_build_object('id',ms.id,'name',s.name,'skill_id',s.id,'category',g.name)
 FROM matrix_skills ms JOIN skills s ON s.id=ms.skill_id JOIN competency_groups g ON g.id=ms.group_id
 WHERE ms.matrix_version_id='00000000-0000-0000-0000-000000000100' ORDER BY ms.position,ms.id;
```

```text
Sort  (cost=78.06..79.31 rows=500 width=52) (actual time=1.044..1.062 rows=500 loops=1)
  Sort Key: ms."position", ms.id
  Sort Method: quicksort  Memory: 118kB
  Buffers: shared hit=29
  ->  Nested Loop  (cost=19.50..55.65 rows=500 width=52) (actual time=0.152..0.958 rows=500 loops=1)
        Join Filter: (g.id = ms.group_id)
        Buffers: shared hit=23
        ->  Seq Scan on competency_groups g  (cost=0.00..1.01 rows=1 width=28) (actual time=0.003..0.003 rows=1 loops=1)
              Buffers: shared hit=1
        ->  Hash Join  (cost=19.50..47.14 rows=500 width=61) (actual time=0.132..0.316 rows=500 loops=1)
              Hash Cond: (s.id = ms.skill_id)
              Buffers: shared hit=22
              ->  Seq Scan on skills s  (cost=0.00..25.00 rows=1000 width=25) (actual time=0.002..0.076 rows=1000 loops=1)
                    Buffers: shared hit=15
              ->  Hash  (cost=13.25..13.25 rows=500 width=52) (actual time=0.117..0.118 rows=500 loops=1)
                    Buckets: 1024  Batches: 1  Memory Usage: 50kB
                    Buffers: shared hit=7
                    ->  Seq Scan on matrix_skills ms  (cost=0.00..13.25 rows=500 width=52) (actual time=0.007..0.063 rows=500 loops=1)
                          Filter: (matrix_version_id = '00000000-0000-0000-0000-000000000100'::uuid)
                          Buffers: shared hit=7
Planning:
  Buffers: shared hit=251
Planning Time: 0.826 ms
Execution Time: 1.154 ms
```

## Export effective requirements

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT jsonb_build_object('matrix_skill_id',r.matrix_skill_id,'level_id',r.level_id,'description',COALESCE(o.description,r.description)) FROM requirements r LEFT JOIN personal_overrides o ON o.requirement_id=r.id AND o.user_matrix_id='00000000-0000-0000-0000-000000001000'::uuid WHERE r.matrix_version_id='00000000-0000-0000-0000-000000000100' ORDER BY r.id;
```

```text
Merge Left Join  (cost=6.85..468.17 rows=5000 width=48) (actual time=0.104..6.820 rows=5000 loops=1)
  Merge Cond: (r.id = o.requirement_id)
  Buffers: shared hit=4984
  ->  Index Scan using requirements_matrix_version_id_id_key on requirements r  (cost=0.28..435.10 rows=5000 width=71) (actual time=0.042..1.356 rows=5000 loops=1)
        Index Cond: (matrix_version_id = '00000000-0000-0000-0000-000000000100'::uuid)
        Buffers: shared hit=4982
  ->  Sort  (cost=6.57..6.82 rows=100 width=39) (actual time=0.050..0.057 rows=100 loops=1)
        Sort Key: o.requirement_id
        Sort Method: quicksort  Memory: 30kB
        Buffers: shared hit=2
        ->  Seq Scan on personal_overrides o  (cost=0.00..3.25 rows=100 width=39) (actual time=0.005..0.014 rows=100 loops=1)
              Filter: (user_matrix_id = '00000000-0000-0000-0000-000000001000'::uuid)
              Buffers: shared hit=2
Planning:
  Buffers: shared hit=185
Planning Time: 0.694 ms
Execution Time: 7.121 ms
```

## Export latest assessment

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT jsonb_build_object('assessment_id',a.id,'period',a.period,'type',a.type,'skill',s.name,'requirement_id',ai.requirement_id,'requirement',ai.requirement_snapshot,'score',ai.score,'status',ai.status,'comment',ai.comment,'na_reason',ai.na_reason)
 FROM assessments a JOIN assessment_items ai ON ai.assessment_id=a.id JOIN requirements r ON r.id=ai.requirement_id JOIN matrix_skills ms ON ms.id=r.matrix_skill_id JOIN skills s ON s.id=ms.skill_id
 WHERE a.id=(SELECT id FROM assessments WHERE user_id='00000000-0000-0000-0000-000000000001' AND user_matrix_id='00000000-0000-0000-0000-000000001000' ORDER BY created_at DESC,id DESC LIMIT 1)
 ORDER BY ms.position,r.id LIMIT 2001;
```

```text
Limit  (cost=252.73..253.98 rows=500 width=52) (actual time=3.162..3.215 rows=500 loops=1)
  Buffers: shared hit=121
  InitPlan 1
    ->  Limit  (cost=1.02..1.03 rows=1 width=24) (actual time=0.018..0.019 rows=1 loops=1)
          Buffers: shared hit=4
          ->  Sort  (cost=1.02..1.03 rows=1 width=24) (actual time=0.018..0.018 rows=1 loops=1)
                Sort Key: assessments.created_at DESC, assessments.id DESC
                Sort Method: quicksort  Memory: 25kB
                Buffers: shared hit=4
                ->  Seq Scan on assessments  (cost=0.00..1.01 rows=1 width=24) (actual time=0.002..0.003 rows=1 loops=1)
                      Filter: ((user_id = '00000000-0000-0000-0000-000000000001'::uuid) AND (user_matrix_id = '00000000-0000-0000-0000-000000001000'::uuid))
                      Buffers: shared hit=1
  ->  Sort  (cost=251.70..252.95 rows=500 width=52) (actual time=3.161..3.184 rows=500 loops=1)
        Sort Key: ms."position", r.id
        Sort Method: quicksort  Memory: 278kB
        Buffers: shared hit=121
        ->  Hash Join  (cost=74.25..229.28 rows=500 width=52) (actual time=0.466..3.018 rows=500 loops=1)
              Hash Cond: (ms.skill_id = s.id)
              Buffers: shared hit=115
              ->  Hash Join  (cost=36.75..189.22 rows=500 width=112) (actual time=0.266..1.127 rows=500 loops=1)
                    Hash Cond: (r.matrix_skill_id = ms.id)
                    Buffers: shared hit=100
                    ->  Nested Loop  (cost=18.50..169.65 rows=500 width=108) (actual time=0.165..0.919 rows=500 loops=1)
                          Buffers: shared hit=93
                          ->  Seq Scan on assessments a  (cost=0.00..1.01 rows=1 width=29) (actual time=0.023..0.024 rows=1 loops=1)
                                Filter: (id = (InitPlan 1).col1)
                                Buffers: shared hit=5
                          ->  Hash Join  (cost=18.50..163.64 rows=500 width=95) (actual time=0.139..0.850 rows=500 loops=1)
                                Hash Cond: (r.id = ai.requirement_id)
                                Buffers: shared hit=88
                                ->  Seq Scan on requirements r  (cost=0.00..132.00 rows=5000 width=32) (actual time=0.002..0.302 rows=5000 loops=1)
                                      Buffers: shared hit=82
                                ->  Hash  (cost=12.25..12.25 rows=500 width=63) (actual time=0.130..0.130 rows=500 loops=1)
                                      Buckets: 1024  Batches: 1  Memory Usage: 57kB
                                      Buffers: shared hit=6
                                      ->  Seq Scan on assessment_items ai  (cost=0.00..12.25 rows=500 width=63) (actual time=0.004..0.056 rows=500 loops=1)
                                            Filter: (assessment_id = (InitPlan 1).col1)
                                            Buffers: shared hit=6
                    ->  Hash  (cost=12.00..12.00 rows=500 width=36) (actual time=0.094..0.094 rows=500 loops=1)
                          Buckets: 1024  Batches: 1  Memory Usage: 42kB
                          Buffers: shared hit=7
                          ->  Seq Scan on matrix_skills ms  (cost=0.00..12.00 rows=500 width=36) (actual time=0.003..0.044 rows=500 loops=1)
                                Buffers: shared hit=7
              ->  Hash  (cost=25.00..25.00 rows=1000 width=25) (actual time=0.180..0.180 rows=1000 loops=1)
                    Buckets: 1024  Batches: 1  Memory Usage: 65kB
                    Buffers: shared hit=15
                    ->  Seq Scan on skills s  (cost=0.00..25.00 rows=1000 width=25) (actual time=0.005..0.091 rows=1000 loops=1)
                          Buffers: shared hit=15
Planning:
  Buffers: shared hit=410
Planning Time: 1.591 ms
Execution Time: 3.350 ms
```

## Export owned evidence

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT jsonb_build_object('id',e.id,'title',e.title,'description',e.description,'fact_type',e.fact_type,'period_from',e.period_from,'period_to',e.period_to,'source',e.source,'source_url',e.source_url,'status',e.status,'skill',s.name,'requirement_id',em.requirement_id,'confidence',em.confidence,'reason',em.reason)
 FROM evidence e JOIN evidence_matches em ON em.evidence_id=e.id JOIN skills s ON s.id=em.skill_id
 WHERE e.user_id='00000000-0000-0000-0000-000000000001' AND EXISTS(SELECT 1 FROM matrix_skills ms WHERE ms.matrix_version_id='00000000-0000-0000-0000-000000000100' AND ms.skill_id=em.skill_id)
 ORDER BY e.id,em.id LIMIT 2001;
```

```text
Limit  (cost=2.32..2436.61 rows=2001 width=64) (actual time=0.639..17.831 rows=2001 loops=1)
  Buffers: shared hit=8991
  ->  Incremental Sort  (cost=2.32..6084.99 rows=5000 width=64) (actual time=0.638..17.691 rows=2001 loops=1)
        Sort Key: e.id, em.id
        Presorted Key: e.id
        Full-sort Groups: 63  Sort Method: quicksort  Average Memory: 57kB  Peak Memory: 57kB
        Buffers: shared hit=8991
        ->  Nested Loop  (cost=1.14..5859.99 rows=5000 width=64) (actual time=0.088..16.617 rows=2002 loops=1)
              Buffers: shared hit=8991
              ->  Nested Loop  (cost=0.86..5448.93 rows=10000 width=378) (actual time=0.053..6.442 rows=2002 loops=1)
                    Buffers: shared hit=7512
                    ->  Merge Join  (cost=0.57..5051.32 rows=10000 width=362) (actual time=0.027..4.929 rows=2002 loops=1)
                          Merge Cond: (e.id = em.evidence_id)
                          Buffers: shared hit=6033
                          ->  Index Scan using evidence_user on evidence e  (cost=0.29..2880.04 rows=10000 width=291) (actual time=0.014..1.552 rows=2002 loops=1)
                                Index Cond: (user_id = '00000000-0000-0000-0000-000000000001'::uuid)
                                Buffers: shared hit=2019
                          ->  Index Scan using matches_evidence on evidence_matches em  (cost=0.29..1996.28 rows=20000 width=87) (actual time=0.009..2.408 rows=4002 loops=1)
                                Buffers: shared hit=4014
                    ->  Memoize  (cost=0.28..0.31 rows=1 width=16) (actual time=0.001..0.001 rows=1 loops=2002)
                          Cache Key: em.skill_id
                          Cache Mode: logical
                          Hits: 1509  Misses: 493  Evictions: 0  Overflows: 0  Memory Usage: 62kB
                          Buffers: shared hit=1479
                          ->  Index Only Scan using matrix_skills_matrix_version_id_skill_id_key on matrix_skills ms  (cost=0.27..0.30 rows=1 width=16) (actual time=0.001..0.001 rows=1 loops=493)
                                Index Cond: ((matrix_version_id = '00000000-0000-0000-0000-000000000100'::uuid) AND (skill_id = em.skill_id))
                                Heap Fetches: 493
                                Buffers: shared hit=1479
              ->  Memoize  (cost=0.29..0.31 rows=1 width=25) (actual time=0.001..0.001 rows=1 loops=2002)
                    Cache Key: em.skill_id
                    Cache Mode: logical
                    Hits: 1509  Misses: 493  Evictions: 0  Overflows: 0  Memory Usage: 67kB
                    Buffers: shared hit=1479
                    ->  Index Scan using skills_pkey on skills s  (cost=0.28..0.30 rows=1 width=25) (actual time=0.001..0.001 rows=1 loops=493)
                          Index Cond: (id = em.skill_id)
                          Buffers: shared hit=1479
Planning:
  Buffers: shared hit=376
Planning Time: 1.768 ms
Execution Time: 18.086 ms
```

## Reproduce the fixture

Use a disposable PostgreSQL 17 database, apply the repository migrations, run the following fixture, then the queries above. Do not run the fixture against an existing application database.

```sql
INSERT INTO users(id,email,name,password_hash) VALUES ('00000000-0000-0000-0000-000000000001','owner@example.invalid','Owner','x'),('00000000-0000-0000-0000-000000000002','other@example.invalid','Other','x');
INSERT INTO skill_categories(key,name) VALUES('engineering_core','Engineering');
INSERT INTO fact_types(key,name) VALUES('project','Project');
INSERT INTO skills(id,key,name,category,scope) SELECT md5('skill-'||n)::uuid,'skill.'||n,'Skill '||n,'engineering_core','system' FROM generate_series(1,1000) n;
INSERT INTO matrices(id,name,scope,visibility) VALUES('00000000-0000-0000-0000-000000000010','Backend','system','public');
INSERT INTO matrix_versions(id,matrix_id,number,status) VALUES('00000000-0000-0000-0000-000000000100','00000000-0000-0000-0000-000000000010',1,'draft');
INSERT INTO levels(id,matrix_version_id,key,name,position) SELECT md5('level-'||n)::uuid,'00000000-0000-0000-0000-000000000100','level.'||n,'Level '||n,n FROM generate_series(1,10) n;
INSERT INTO competency_groups(id,matrix_version_id,key,name,weight) VALUES('00000000-0000-0000-0000-000000000200','00000000-0000-0000-0000-000000000100','engineering','Engineering',1);
INSERT INTO matrix_skills(id,matrix_version_id,skill_id,group_id,weight,required,active,position) SELECT md5('ms-'||n)::uuid,'00000000-0000-0000-0000-000000000100',md5('skill-'||n)::uuid,'00000000-0000-0000-0000-000000000200',1,false,true,n FROM generate_series(1,500) n;
INSERT INTO requirements(id,matrix_version_id,matrix_skill_id,level_id,description,required,critical,weight) SELECT md5('req-'||s||'-'||l)::uuid,'00000000-0000-0000-0000-000000000100',md5('ms-'||s)::uuid,md5('level-'||l)::uuid,'Requirement '||s||' level '||l,false,false,1 FROM generate_series(1,500) s CROSS JOIN generate_series(1,10) l;
UPDATE matrix_versions SET status='published' WHERE id='00000000-0000-0000-0000-000000000100';
INSERT INTO user_matrices(id,user_id,matrix_version_id,current_level_id,target_level_id) VALUES('00000000-0000-0000-0000-000000001000','00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000100',md5('level-1')::uuid,md5('level-10')::uuid);
INSERT INTO evidence(id,user_id,title,description,fact_type,source,external_id,status,created_by,import_hash)
SELECT md5('evidence-'||n)::uuid,CASE WHEN n<=10000 THEN '00000000-0000-0000-0000-000000000001'::uuid ELSE '00000000-0000-0000-0000-000000000002'::uuid END,'Evidence '||n,repeat('Representative description. ',8),'project','github','external-'||n,'accepted','00000000-0000-0000-0000-000000000001',repeat('a',64) FROM generate_series(1,20000) n;
INSERT INTO evidence_matches(id,evidence_id,skill_id,requirement_id,confidence,reason,created_by)
SELECT md5('match-'||n)::uuid,md5('evidence-'||n)::uuid,md5('skill-'||((n-1)%500+1))::uuid,md5('req-'||((n-1)%500+1)||'-1')::uuid,0.9,'Matched source','00000000-0000-0000-0000-000000000001' FROM generate_series(1,20000) n;
INSERT INTO personal_overrides(user_matrix_id,requirement_id,description,reason) SELECT '00000000-0000-0000-0000-000000001000',md5('req-'||n||'-1')::uuid,'Personal requirement '||n,'Context' FROM generate_series(1,100) n;
INSERT INTO assessments(id,user_id,user_matrix_id,matrix_version_id,period,type) VALUES('00000000-0000-0000-0000-000000002000','00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000001000','00000000-0000-0000-0000-000000000100','2026-Q3','self');
INSERT INTO assessment_items(assessment_id,requirement_id,score,status,requirement_snapshot) SELECT '00000000-0000-0000-0000-000000002000',md5('req-'||n||'-1')::uuid,3,'assessed','Snapshot '||n FROM generate_series(1,500) n;
ANALYZE;
```
