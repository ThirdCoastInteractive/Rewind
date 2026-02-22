-- InsertVideoRevision stores a refresh diff.
-- name: InsertVideoRevision :exec
INSERT INTO video_revisions (
    video_id,
    kind,
    diff,
    old_title,
    new_title,
    old_description,
    new_description,
    old_info,
    new_info
)
VALUES (
    sqlc.arg(video_id),
    sqlc.arg(kind),
    sqlc.arg(diff),
    sqlc.narg(old_title),
    sqlc.narg(new_title),
    sqlc.narg(old_description),
    sqlc.narg(new_description),
    sqlc.narg(old_info),
    sqlc.narg(new_info)
);
