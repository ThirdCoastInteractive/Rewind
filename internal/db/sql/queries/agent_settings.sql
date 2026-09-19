-- name: ListRuntimeSettings :many
SELECT * FROM runtime_settings ORDER BY key;
-- name: AgentUsesModel :one
SELECT EXISTS(SELECT 1 FROM agent_runs WHERE status IN ('queued','running','waiting_capacity','waiting_input','waiting_approval') AND settings->>'agent.model'=$1::text)::boolean;
-- name: CaptureJobConfiguration :one
INSERT INTO job_configuration(kind,job_id,snapshot) VALUES($1,$2,$3) ON CONFLICT(kind,job_id) DO UPDATE SET job_id=EXCLUDED.job_id RETURNING snapshot;
-- name: SeedRuntimeSetting :exec
INSERT INTO runtime_settings(key,value,source) VALUES($1,$2,'environment') ON CONFLICT DO NOTHING;
-- name: SaveRuntimeSetting :one
INSERT INTO runtime_settings(key,value,updated_by) VALUES($1,$2,$3)
ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,source='user',updated_by=EXCLUDED.updated_by,revision=runtime_settings.revision+1,updated_at=now() RETURNING *;
-- name: AckRuntimeSettings :exec
INSERT INTO runtime_settings_consumers(service,snapshot,hostname,stopped_at)
VALUES($1,$2,$3,NULL)
ON CONFLICT(service) DO UPDATE SET
    snapshot=EXCLUDED.snapshot,
    hostname=EXCLUDED.hostname,
    stopped_at=NULL,
    updated_at=now();
-- name: StopRuntimeSettings :exec
UPDATE runtime_settings_consumers SET stopped_at=now(), updated_at=now() WHERE service=$1 AND stopped_at IS NULL;
-- name: ListRuntimeConsumers :many
SELECT * FROM runtime_settings_consumers
WHERE stopped_at IS NULL
  AND updated_at > now() - interval '3 minutes'
ORDER BY service;
-- name: GetInterfacePreferences :one
SELECT preferences FROM user_interface_preferences WHERE user_id=$1;
-- name: SaveInterfacePreferences :exec
INSERT INTO user_interface_preferences(user_id,preferences) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET preferences=EXCLUDED.preferences,updated_at=now();
-- name: MergeInterfacePreferences :exec
INSERT INTO user_interface_preferences(user_id,preferences) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET preferences=user_interface_preferences.preferences || EXCLUDED.preferences,updated_at=now();
-- name: CreateAgentConversation :one
INSERT INTO agent_conversations(user_id,title) VALUES($1,$2) RETURNING *;
-- name: ListAgentConversations :many
SELECT c.* FROM agent_conversations c WHERE c.user_id=$1 ORDER BY COALESCE((SELECT max(r.updated_at) FROM agent_runs r WHERE r.conversation_id=c.id),c.updated_at) DESC LIMIT 100;
-- name: GetAgentConversation :one
SELECT * FROM agent_conversations WHERE id=$1 AND user_id=$2;
-- name: LatestAgentRun :one
SELECT * FROM agent_runs WHERE conversation_id=$1 AND user_id=$2 ORDER BY created_at DESC LIMIT 1;
-- name: CreateAgentRun :one
INSERT INTO agent_runs(conversation_id,user_id,messages,settings) SELECT c.id,c.user_id,sqlc.arg(messages)::jsonb,sqlc.arg(settings)::jsonb FROM agent_conversations c WHERE c.id=sqlc.arg(conversation_id) AND c.user_id=sqlc.arg(user_id) RETURNING *;
-- name: GetAgentRun :one
SELECT * FROM agent_runs WHERE id=$1 AND user_id=$2;
-- name: ContinueAgentRun :one
INSERT INTO agent_runs(conversation_id,user_id,messages,settings,model_digest,runtime,artifacts)
SELECT r.conversation_id,r.user_id,sqlc.arg(messages)::jsonb,r.settings,r.model_digest,r.runtime,r.artifacts
FROM agent_runs r WHERE r.id=sqlc.arg(id) AND r.user_id=sqlc.arg(user_id)
AND r.status IN ('limited','interrupted','cancelled')
AND NOT EXISTS(SELECT 1 FROM agent_runs newer WHERE newer.conversation_id=r.conversation_id AND newer.created_at>r.created_at)
RETURNING *;
-- name: ClaimAgentRun :one
UPDATE agent_runs SET status='running',lease_owner=$1,lease_until=now()+interval '60 seconds',updated_at=now()
WHERE id=(SELECT id FROM agent_runs WHERE agent_runs.runtime=sqlc.arg(runtime) AND status IN ('queued','running','waiting_capacity') AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *;
-- name: HeartbeatAgentRun :one
UPDATE agent_runs SET lease_until=now()+interval '60 seconds' WHERE id=$1 AND lease_owner=$2 AND status='running' AND lease_until>now() RETURNING cancel_requested;
-- name: CheckpointAgentRun :execrows
UPDATE agent_runs SET messages=$3,calls=$4,model_digest=$5,updated_at=now() WHERE id=$1 AND lease_owner=$2 AND status='running' AND lease_until>now();
-- name: FinishAgentRun :execrows
UPDATE agent_runs SET status=$3,last_error=$4,cancellation_acknowledged=($3='cancelled'),lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_owner=$2 AND status='running' AND lease_until>now();
-- name: CancelAgentRun :exec
UPDATE agent_runs SET cancel_requested=true,updated_at=now() WHERE id=$1 AND user_id=$2;
-- name: AddAgentEvent :execrows
INSERT INTO agent_events(run_id,kind,data) SELECT r.id,sqlc.arg(kind),sqlc.arg(data)::jsonb FROM agent_runs r WHERE r.id=sqlc.arg(run_id) AND r.lease_owner=sqlc.arg(lease_owner) AND r.status='running' AND r.lease_until>now();
-- name: AddAgentArtifact :exec
UPDATE agent_runs SET artifacts=artifacts || jsonb_build_array(sqlc.arg(artifact)::jsonb) WHERE id=sqlc.arg(id) AND lease_owner=sqlc.arg(lease_owner) AND status='running' AND lease_until>now() AND NOT artifacts @> jsonb_build_array(sqlc.arg(artifact)::jsonb);
-- name: ListAgentEvents :many
SELECT e.* FROM agent_events e JOIN agent_runs r ON r.id=e.run_id WHERE e.run_id=$1 AND r.user_id=$2 AND e.id>$3 ORDER BY e.id LIMIT 500;
-- name: StartAgentToolCall :one
INSERT INTO agent_tool_calls(run_id,call_index,name,arguments) SELECT r.id,sqlc.arg(call_index)::integer,sqlc.arg(name)::text,sqlc.arg(arguments)::jsonb FROM agent_runs r WHERE r.id=sqlc.arg(run_id) AND r.lease_owner=sqlc.arg(lease_owner) AND r.status='running' AND r.lease_until>now() AND NOT r.cancel_requested RETURNING *;
-- name: FinishAgentToolCall :execrows
UPDATE agent_tool_calls c SET status='completed',result=sqlc.arg(result)::jsonb FROM agent_runs r WHERE c.id=sqlc.arg(id) AND r.id=c.run_id AND r.lease_owner=sqlc.arg(lease_owner) AND r.status='running' AND r.lease_until>now();
-- name: ListAgentToolCalls :many
SELECT * FROM agent_tool_calls WHERE run_id=$1 ORDER BY call_index;
-- name: CreateModelOperation :one
INSERT INTO model_operations(user_id,runtime,model,action,options) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: ClaimModelOperation :one
UPDATE model_operations SET status='running',updated_at=now() WHERE id=(SELECT id FROM model_operations WHERE status='queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *;
-- name: UpdateModelOperation :exec
UPDATE model_operations SET status=$2,progress=$3,updated_at=now() WHERE id=$1;
-- name: ListModelOperations :many
SELECT * FROM model_operations ORDER BY created_at DESC LIMIT 100;
-- name: RecoverModelOperations :exec
UPDATE model_operations SET status='interrupted',updated_at=now() WHERE status='running';
