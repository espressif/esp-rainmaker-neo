# Espressif user management

A comprehensive, secure, and scalable user management system that can be integrated with any Espressif product. It is designed to be used across regions and AWS accounts.


## Table of Contents

- [Espressif user management stack](#espressif-user-management-stack)
  - [Table of Contents](#table-of-contents)
  - [Inputs from user](#inputs-from-user)
  - [Resources](#resources)
    - [User](#user)
    - [Admin](#admin)
  - [Privacy and security](#privacy-and-security)
  - [Flags](#flags)
  - [Outputs](#outputs)
  - [Security \& Privacy](#security--privacy)
    - [Data Encryption](#data-encryption)
- [🚀 Deployment \& Operations](#-deployment--operations)
  - [Configuration](#configuration)
    - [Environment Variables](#environment-variables)
    - [Database Configuration](#database-configuration)
      - [DynamoDB (Recommended)](#dynamodb-recommended)
      - [PostgreSQL](#postgresql)
  - [Deployment Options](#deployment-options)
    - [AWS Serverless Deployment](#aws-serverless-deployment)
    - [Docker Deployment](#docker-deployment)
    - [Kubernetes Deployment](#kubernetes-deployment)
  - [Monitoring \& Analytics](#monitoring--analytics)
    - [User Statistics](#user-statistics)
    - [Health Monitoring](#health-monitoring)
    - [Audit Logging](#audit-logging)
  - [Migration Tools](#migration-tools)
    - [From Existing User System](#from-existing-user-system)
    - [Gradual Migration](#gradual-migration)
- [🔧 Customization \& Support](#-customization--support)
  - [Template Customization](#template-customization)
    - [Email Templates](#email-templates)
    - [SMS Templates](#sms-templates)
    - [Custom User Fields](#custom-user-fields)
  - [Troubleshooting](#troubleshooting)
    - [Common Issues](#common-issues)
      - [1. Authentication Failures](#1-authentication-failures)
      - [2. Database Connection Issues](#2-database-connection-issues)
      - [3. Rate Limit Issues](#3-rate-limit-issues)
      - [4. OAuth Integration Issues](#4-oauth-integration-issues)
    - [Debug Mode](#debug-mode)
    - [Performance Tuning](#performance-tuning)
  - [Support \& Contributing](#support--contributing)
    - [Getting Help](#getting-help)
    - [Contributing](#contributing)
      - [Development Setup](#development-setup)
      - [Code Style](#code-style)
    - [License](#license)

## Inputs from user

- Default superadmin user email (can be changed later)

## Resources

### User

- Cognito User Pool
- A default root superadmin user
- APIs:
  - User
    - Signup (Can there be a link for verification?)
    - Login (email/phone + password OR OTP based or SRP)
    - Forgot and change password
    - Update profile: locale, name, email, phone number, picture, preferences, custom data (can be set by each product)
    - Get user
    - MFA: enable, disable (TODO: required?)
    - Signout
    - Export/Email data
    - Delete account
    - Link accounts
    - Get active sessions, revoke session (session_id, created_at, last_used, ip_address, user_agent, current)
    - Publish events to be consumed by webhooks

TODO: Custom OAuth?
TODO: Scope

### Admin

- Cognito User Pool
- Cognito User Pool Clients for dashboard and admin cli - TODO: configurable?
- Cognito User Pool Domain - TODO: required?
- A default superadmin user
- APIs:
  - Own account
    - Same as [User](README.md#user)
    - Change superadmin user
  - User accounts
    - User management CRUD (including search by tags, email, phone number, name, active users, created_at, ) (Includes cloud to cloud auth)
    - RBAC for admins
    - Admin editable custom data and tags per user
    - User session logs
    - Purge inactive users
    - User groups?
    - Aanlytics (active users, new users)
  - Configuration
    - Cognito domain CRUD for both pools (TODO: Required for admin pool?)
    - App clients CRUD for both pools (Clients for apps, dashboard, CLI, alexa and gva - TODO: configurable?)
    - Identity providers CRUD for user pool (Providers for google, apple, github, amazon, facebook, any other oidc provider)
    - Email/SMS templates CRUD
    - Email/SMS service logs
    - SES mail sender CRUD
  - Bulk user accounts
    - Import users with password
    - Bulk updates
    - Gradual migration: user validation and creation against an existing system

TODO: Sub admin

## Privacy and security
- PKCE for OAuth
- Password policy
- Data encryption
- Data retention
- Token expiration and rotation

## Flags
Keep flags for notify, etc.

## Outputs

- API gateway URL
- Admin Cognito OAuth ARN and URL
- Admin Cognito user pool client Id and secret
- User Cognito OAuth ARN and URL
- User Cognito user pool client Id and secret