-- Marionette Database Schema
-- PostgreSQL 15+
--
-- Migration: 004_builtin_profiles
-- Description: Add built-in profiles for common use cases

-- Insert built-in profiles with is_builtin = true
-- These profiles have NULL tenant_id (global) and cannot be deleted

INSERT INTO profiles (id, name, description, resources, network, tunnels, labels, is_builtin, created_at, updated_at)
VALUES
    -- dev-small: Basic development environment
    (
        'prof_builtin_dev_small',
        'dev-small',
        'Small development environment for basic tasks',
        '{"cpu": "2", "memory": "4GB", "disk": "20GB"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.github.com", "*.npmjs.org", "*.pypi.org", "api.anthropic.com", "api.openai.com"]}'::jsonb,
        '[]'::jsonb,
        '{"purpose": "development", "tier": "small"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- dev-medium: General purpose development environment
    (
        'prof_builtin_dev_medium',
        'dev-medium',
        'Medium development environment for general purpose tasks',
        '{"cpu": "4", "memory": "8GB", "disk": "50GB"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.github.com", "*.npmjs.org", "*.pypi.org", "*.golang.org", "api.anthropic.com", "api.openai.com"]}'::jsonb,
        '[]'::jsonb,
        '{"purpose": "development", "tier": "medium"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- dev-large: High-resource development environment
    (
        'prof_builtin_dev_large',
        'dev-large',
        'Large development environment for resource-intensive tasks',
        '{"cpu": "8", "memory": "16GB", "disk": "100GB"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.github.com", "*.npmjs.org", "*.pypi.org", "*.golang.org", "api.anthropic.com", "api.openai.com"]}'::jsonb,
        '[]'::jsonb,
        '{"purpose": "development", "tier": "large"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- ml-gpu: Machine learning with GPU support
    (
        'prof_builtin_ml_gpu',
        'ml-gpu',
        'Machine learning environment with GPU support',
        '{"cpu": "8", "memory": "32GB", "disk": "200GB", "gpu": "1"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.github.com", "*.huggingface.co", "api.anthropic.com", "api.openai.com"]}'::jsonb,
        '[]'::jsonb,
        '{"purpose": "ml", "tier": "gpu", "requires": "gpu"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- web-dev: Web development with browser support
    (
        'prof_builtin_web_dev',
        'web-dev',
        'Web development environment with browser streaming',
        '{"cpu": "4", "memory": "8GB", "disk": "50GB"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.github.com", "*.npmjs.org", "*.unpkg.com", "*.cdnjs.com", "api.anthropic.com"]}'::jsonb,
        '[{"type": "browser", "auto": true}, {"type": "desktop", "auto": true}]'::jsonb,
        '{"purpose": "web-development"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- android-dev: Android development with emulator
    (
        'prof_builtin_android_dev',
        'android-dev',
        'Android development environment with emulator support',
        '{"cpu": "4", "memory": "8GB", "disk": "50GB"}'::jsonb,
        '{"policy": "allow_list", "allowed_hosts": ["*.android.com", "*.google.com", "*.github.com", "maven.google.com"]}'::jsonb,
        '[{"type": "android", "auto": true}]'::jsonb,
        '{"purpose": "android-development"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    ),
    -- secure: Air-gapped environment for sensitive tasks
    (
        'prof_builtin_secure',
        'secure',
        'Air-gapped environment for sensitive and secure tasks',
        '{"cpu": "2", "memory": "4GB", "disk": "20GB"}'::jsonb,
        '{"policy": "air_gapped"}'::jsonb,
        '[]'::jsonb,
        '{"purpose": "secure", "compliance": "high"}'::jsonb,
        TRUE,
        NOW(),
        NOW()
    )
ON CONFLICT DO NOTHING;
