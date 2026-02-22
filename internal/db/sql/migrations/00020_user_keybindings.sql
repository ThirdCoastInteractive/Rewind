-- +goose Up
CREATE TABLE user_keybindings (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    key TEXT NOT NULL,
    PRIMARY KEY (user_id, action)
);

-- +goose Down
DROP TABLE user_keybindings;
