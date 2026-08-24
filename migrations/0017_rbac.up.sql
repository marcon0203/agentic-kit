CREATE TABLE roles (
    id          BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    role_key    VARCHAR(32)  NOT NULL UNIQUE,
    name        VARCHAR(64)  NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Permissions are code-defined capabilities, one per gated button/action —
-- rows are seeded below and managed by migrations, not created through the
-- API. What an admin actually configures is which permissions a Role holds
-- and which Roles a user has.
CREATE TABLE permissions (
    id             BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    permission_key VARCHAR(64)  NOT NULL UNIQUE,
    name           VARCHAR(128) NOT NULL,
    module         VARCHAR(32)  NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_id       BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_role_permissions_permission_id ON role_permissions(permission_id);
CREATE INDEX idx_user_roles_role_id ON user_roles(role_id);

INSERT INTO permissions (permission_key, name, module) VALUES
    ('iam.user.view', '查看用户列表', 'iam'),
    ('iam.user.manage_status', '启用/停用用户', 'iam'),
    ('iam.user.manage_roles', '分配用户角色', 'iam'),
    ('iam.role.view', '查看角色列表', 'iam'),
    ('iam.role.manage', '新增/删除角色、编辑角色权限', 'iam'),
    ('model_catalog.provider.view', '查看模型 Provider 列表', 'model_catalog'),
    ('model_catalog.provider.create', '新增模型 Provider', 'model_catalog'),
    ('model_catalog.provider.toggle', '启用/停用模型 Provider', 'model_catalog'),
    ('model_catalog.provider.delete', '删除模型 Provider', 'model_catalog'),
    ('model_catalog.model.create', '新增模型', 'model_catalog'),
    ('model_catalog.model.toggle', '启用/停用模型', 'model_catalog'),
    ('model_catalog.model.delete', '删除模型', 'model_catalog');
