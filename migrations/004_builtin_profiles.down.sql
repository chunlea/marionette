-- Marionette Database Schema
-- PostgreSQL 15+
--
-- Migration: 004_builtin_profiles (down)
-- Description: Remove built-in profiles

DELETE FROM profiles WHERE is_builtin = TRUE AND id LIKE 'prof_builtin_%';
