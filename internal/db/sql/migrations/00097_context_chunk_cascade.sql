-- +goose Up
-- Deleting a recording cascades through context_window_sets. Chunks must
-- go with the set, or the video delete fails and the library row stays.
ALTER TABLE context_window_chunks DROP CONSTRAINT context_window_chunks_set_id_fkey;
ALTER TABLE context_window_chunks
    ADD CONSTRAINT context_window_chunks_set_id_fkey
    FOREIGN KEY (set_id) REFERENCES context_window_sets(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE context_window_chunks DROP CONSTRAINT context_window_chunks_set_id_fkey;
ALTER TABLE context_window_chunks
    ADD CONSTRAINT context_window_chunks_set_id_fkey
    FOREIGN KEY (set_id) REFERENCES context_window_sets(id);
