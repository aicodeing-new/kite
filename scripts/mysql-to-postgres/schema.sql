--
-- PostgreSQL database dump
--


-- Dumped from database version 17.10
-- Dumped by pg_dump version 17.11 (Debian 17.11-1.pgdg13+2)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: access_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.access_requests (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    requester_id bigint NOT NULL,
    requester_name character varying(100),
    cluster character varying(255),
    namespace character varying(255) NOT NULL,
    request_type character varying(30) DEFAULT ''::character varying NOT NULL,
    report_link character varying(500),
    target_resources text,
    duration_hours bigint NOT NULL,
    risk_level character varying(50) DEFAULT 'low'::character varying NOT NULL,
    reason text,
    approver_uid character varying(255),
    approver_name character varying(100),
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    expires_at timestamp with time zone,
    approved_at timestamp with time zone,
    ended_at timestamp with time zone,
    expiring_soon_notified boolean DEFAULT false NOT NULL,
    message_id character varying(255),
    role_id bigint,
    review_note text,
    summary_status character varying(20),
    summary_attempts bigint DEFAULT 0 NOT NULL,
    summary_ai_attempts bigint DEFAULT 0 NOT NULL,
    summary_delivery_attempts bigint DEFAULT 0 NOT NULL,
    summary_last_error text,
    summary_next_retry_at timestamp with time zone,
    summary_claimed_at timestamp with time zone,
    summary_claim_token character varying(64),
    summary_completed_at timestamp with time zone,
    summary_message_id character varying(255),
    summary_content text,
    summary_stats text
);


--
-- Name: access_requests_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.access_requests_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: access_requests_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.access_requests_id_seq OWNED BY public.access_requests.id;


--
-- Name: clusters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.clusters (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(100) NOT NULL,
    description text,
    config text,
    prometheus_url character varying(255),
    gpu_resource_rules text,
    in_cluster boolean DEFAULT false,
    is_default boolean DEFAULT false,
    enable boolean DEFAULT true
);


--
-- Name: clusters_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.clusters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: clusters_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.clusters_id_seq OWNED BY public.clusters.id;


--
-- Name: feishu_notification_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.feishu_notification_settings (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    app_id character varying(255),
    app_secret text,
    group_chat_id character varying(255),
    verification_token text,
    approvers text,
    enabled boolean DEFAULT false NOT NULL
);


--
-- Name: feishu_notification_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.feishu_notification_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: feishu_notification_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.feishu_notification_settings_id_seq OWNED BY public.feishu_notification_settings.id;


--
-- Name: general_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.general_settings (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    ai_agent_enabled boolean DEFAULT false NOT NULL,
    ai_provider character varying(50) DEFAULT 'openai'::character varying NOT NULL,
    ai_model character varying(255) DEFAULT 'gpt-4o-mini'::character varying NOT NULL,
    ai_api_key text,
    ai_base_url character varying(500),
    ai_max_tokens integer DEFAULT 4096,
    ai_reasoning_effort character varying(20) DEFAULT 'low'::character varying NOT NULL,
    kubectl_enabled boolean DEFAULT true NOT NULL,
    kubectl_image character varying(255) DEFAULT 'zzde/kubectl:latest'::character varying NOT NULL,
    node_terminal_image character varying(255) DEFAULT 'busybox:latest'::character varying NOT NULL,
    enable_version_check boolean DEFAULT true NOT NULL,
    password_login_disabled boolean DEFAULT false NOT NULL,
    jwt_secret text,
    global_sidebar_preference text,
    enable_analytics boolean DEFAULT false
);


--
-- Name: general_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.general_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: general_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.general_settings_id_seq OWNED BY public.general_settings.id;


--
-- Name: ldap_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ldap_settings (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    enabled boolean DEFAULT false NOT NULL,
    server_url character varying(500),
    use_starttls boolean DEFAULT false NOT NULL,
    bind_dn character varying(500),
    bind_password text,
    user_base_dn character varying(500),
    user_filter character varying(500) DEFAULT '(uid=%s)'::character varying,
    username_attribute character varying(100) DEFAULT 'uid'::character varying,
    display_name_attribute character varying(100) DEFAULT 'cn'::character varying,
    group_base_dn character varying(500),
    group_filter character varying(500) DEFAULT '(member=%s)'::character varying,
    group_name_attribute character varying(100) DEFAULT 'cn'::character varying
);


--
-- Name: ldap_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ldap_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ldap_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ldap_settings_id_seq OWNED BY public.ldap_settings.id;


--
-- Name: o_auth_providers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.o_auth_providers (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(100) NOT NULL,
    client_id character varying(255) NOT NULL,
    client_secret text NOT NULL,
    auth_url character varying(255),
    token_url character varying(255),
    user_info_url character varying(255),
    scopes character varying(255) DEFAULT 'openid,profile,email'::character varying,
    issuer character varying(255),
    enabled boolean DEFAULT true,
    username_claim character varying(255),
    groups_claim character varying(255),
    allowed_groups text
);


--
-- Name: o_auth_providers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.o_auth_providers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: o_auth_providers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.o_auth_providers_id_seq OWNED BY public.o_auth_providers.id;


--
-- Name: pending_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pending_sessions (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    cluster_name character varying(255),
    session_id character varying(64) NOT NULL,
    provider character varying(32) NOT NULL,
    conversation_session_id character varying(64),
    system_prompt text,
    open_ai_messages text,
    anthropic_messages text,
    tool_call_id character varying(255),
    tool_call_name character varying(255),
    tool_call_args text,
    expires_at timestamp with time zone NOT NULL,
    user_key character varying(255)
);


--
-- Name: pending_sessions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.pending_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pending_sessions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.pending_sessions_id_seq OWNED BY public.pending_sessions.id;


--
-- Name: proxy_authorization_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.proxy_authorization_codes (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint NOT NULL,
    code_hash character varying(64),
    challenge character varying(128),
    redirect_uri character varying(255),
    device_name character varying(100),
    expires_at timestamp with time zone
);


--
-- Name: proxy_authorization_codes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.proxy_authorization_codes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: proxy_authorization_codes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.proxy_authorization_codes_id_seq OWNED BY public.proxy_authorization_codes.id;


--
-- Name: proxy_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.proxy_sessions (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint NOT NULL,
    device_name character varying(100),
    access_hash character varying(64),
    refresh_hash character varying(64),
    access_expires_at timestamp with time zone,
    expires_at timestamp with time zone,
    last_seen_at timestamp with time zone,
    revoked_at timestamp with time zone
);


--
-- Name: proxy_sessions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.proxy_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: proxy_sessions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.proxy_sessions_id_seq OWNED BY public.proxy_sessions.id;


--
-- Name: resource_histories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.resource_histories (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    cluster_name character varying(100) NOT NULL,
    resource_type character varying(50) NOT NULL,
    resource_name character varying(255) NOT NULL,
    namespace character varying(100),
    operation_type character varying(50) NOT NULL,
    operation_source character varying(20) DEFAULT 'manual'::character varying NOT NULL,
    resource_yaml text,
    previous_yaml text,
    success boolean,
    error_message text,
    operator_id bigint NOT NULL
);


--
-- Name: resource_histories_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.resource_histories_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: resource_histories_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.resource_histories_id_seq OWNED BY public.resource_histories.id;


--
-- Name: resource_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.resource_templates (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(255) NOT NULL,
    description text,
    yaml text
);


--
-- Name: resource_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.resource_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: resource_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.resource_templates_id_seq OWNED BY public.resource_templates.id;


--
-- Name: role_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_assignments (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    role_id bigint NOT NULL,
    subject_type character varying(20) NOT NULL,
    subject character varying(255) NOT NULL
);


--
-- Name: role_assignments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.role_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: role_assignments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.role_assignments_id_seq OWNED BY public.role_assignments.id;


--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(100) NOT NULL,
    description text,
    is_system boolean DEFAULT false NOT NULL,
    clusters text,
    resources text,
    resource_names text,
    namespaces text,
    verbs text,
    allow_proxy boolean DEFAULT false NOT NULL,
    proxy_namespaces text
);


--
-- Name: roles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: roles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.roles_id_seq OWNED BY public.roles.id;


--
-- Name: user_group_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_group_members (
    user_group_id bigint NOT NULL,
    user_id bigint NOT NULL
);


--
-- Name: user_groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_groups (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    name character varying(100) NOT NULL,
    description text
);


--
-- Name: user_groups_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_groups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_groups_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_groups_id_seq OWNED BY public.user_groups.id;


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    username character varying(50) NOT NULL,
    password character varying(255),
    name character varying(100),
    avatar_url character varying(500),
    provider character varying(50) DEFAULT 'password'::character varying,
    o_id_c_groups text,
    last_login_at timestamp without time zone,
    enabled boolean DEFAULT true,
    sub character varying(255),
    api_key text,
    sidebar_preference text,
    default_cluster character varying(100)
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;


--
-- Name: access_requests id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.access_requests ALTER COLUMN id SET DEFAULT nextval('public.access_requests_id_seq'::regclass);


--
-- Name: clusters id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.clusters ALTER COLUMN id SET DEFAULT nextval('public.clusters_id_seq'::regclass);


--
-- Name: feishu_notification_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feishu_notification_settings ALTER COLUMN id SET DEFAULT nextval('public.feishu_notification_settings_id_seq'::regclass);


--
-- Name: general_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.general_settings ALTER COLUMN id SET DEFAULT nextval('public.general_settings_id_seq'::regclass);


--
-- Name: ldap_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ldap_settings ALTER COLUMN id SET DEFAULT nextval('public.ldap_settings_id_seq'::regclass);


--
-- Name: o_auth_providers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.o_auth_providers ALTER COLUMN id SET DEFAULT nextval('public.o_auth_providers_id_seq'::regclass);


--
-- Name: pending_sessions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pending_sessions ALTER COLUMN id SET DEFAULT nextval('public.pending_sessions_id_seq'::regclass);


--
-- Name: proxy_authorization_codes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_authorization_codes ALTER COLUMN id SET DEFAULT nextval('public.proxy_authorization_codes_id_seq'::regclass);


--
-- Name: proxy_sessions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_sessions ALTER COLUMN id SET DEFAULT nextval('public.proxy_sessions_id_seq'::regclass);


--
-- Name: resource_histories id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.resource_histories ALTER COLUMN id SET DEFAULT nextval('public.resource_histories_id_seq'::regclass);


--
-- Name: resource_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.resource_templates ALTER COLUMN id SET DEFAULT nextval('public.resource_templates_id_seq'::regclass);


--
-- Name: role_assignments id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_assignments ALTER COLUMN id SET DEFAULT nextval('public.role_assignments_id_seq'::regclass);


--
-- Name: roles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles ALTER COLUMN id SET DEFAULT nextval('public.roles_id_seq'::regclass);


--
-- Name: user_groups id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_groups ALTER COLUMN id SET DEFAULT nextval('public.user_groups_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);


--
-- Name: access_requests access_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.access_requests
    ADD CONSTRAINT access_requests_pkey PRIMARY KEY (id);


--
-- Name: clusters clusters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.clusters
    ADD CONSTRAINT clusters_pkey PRIMARY KEY (id);


--
-- Name: feishu_notification_settings feishu_notification_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feishu_notification_settings
    ADD CONSTRAINT feishu_notification_settings_pkey PRIMARY KEY (id);


--
-- Name: general_settings general_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.general_settings
    ADD CONSTRAINT general_settings_pkey PRIMARY KEY (id);


--
-- Name: ldap_settings ldap_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ldap_settings
    ADD CONSTRAINT ldap_settings_pkey PRIMARY KEY (id);


--
-- Name: o_auth_providers o_auth_providers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.o_auth_providers
    ADD CONSTRAINT o_auth_providers_pkey PRIMARY KEY (id);


--
-- Name: pending_sessions pending_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pending_sessions
    ADD CONSTRAINT pending_sessions_pkey PRIMARY KEY (id);


--
-- Name: proxy_authorization_codes proxy_authorization_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_authorization_codes
    ADD CONSTRAINT proxy_authorization_codes_pkey PRIMARY KEY (id);


--
-- Name: proxy_sessions proxy_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_sessions
    ADD CONSTRAINT proxy_sessions_pkey PRIMARY KEY (id);


--
-- Name: resource_histories resource_histories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.resource_histories
    ADD CONSTRAINT resource_histories_pkey PRIMARY KEY (id);


--
-- Name: resource_templates resource_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.resource_templates
    ADD CONSTRAINT resource_templates_pkey PRIMARY KEY (id);


--
-- Name: role_assignments role_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_assignments
    ADD CONSTRAINT role_assignments_pkey PRIMARY KEY (id);


--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);


--
-- Name: user_group_members user_group_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_group_members
    ADD CONSTRAINT user_group_members_pkey PRIMARY KEY (user_group_id, user_id);


--
-- Name: user_groups user_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_groups
    ADD CONSTRAINT user_groups_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: idx_access_requests_requester_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_access_requests_requester_id ON public.access_requests USING btree (requester_id);


--
-- Name: idx_access_requests_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_access_requests_status ON public.access_requests USING btree (status);


--
-- Name: idx_access_requests_summary_claimed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_access_requests_summary_claimed_at ON public.access_requests USING btree (summary_claimed_at);


--
-- Name: idx_access_requests_summary_next_retry_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_access_requests_summary_next_retry_at ON public.access_requests USING btree (summary_next_retry_at);


--
-- Name: idx_access_requests_summary_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_access_requests_summary_status ON public.access_requests USING btree (summary_status);


--
-- Name: idx_clusters_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_clusters_name ON public.clusters USING btree (name);


--
-- Name: idx_o_auth_providers_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_o_auth_providers_name ON public.o_auth_providers USING btree (name);


--
-- Name: idx_pending_sessions_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pending_sessions_expires_at ON public.pending_sessions USING btree (expires_at);


--
-- Name: idx_pending_sessions_session_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_pending_sessions_session_id ON public.pending_sessions USING btree (session_id);


--
-- Name: idx_proxy_authorization_codes_code_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_proxy_authorization_codes_code_hash ON public.proxy_authorization_codes USING btree (code_hash);


--
-- Name: idx_proxy_authorization_codes_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_proxy_authorization_codes_expires_at ON public.proxy_authorization_codes USING btree (expires_at);


--
-- Name: idx_proxy_authorization_codes_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_proxy_authorization_codes_user_id ON public.proxy_authorization_codes USING btree (user_id);


--
-- Name: idx_proxy_sessions_access_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_proxy_sessions_access_hash ON public.proxy_sessions USING btree (access_hash);


--
-- Name: idx_proxy_sessions_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_proxy_sessions_expires_at ON public.proxy_sessions USING btree (expires_at);


--
-- Name: idx_proxy_sessions_refresh_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_proxy_sessions_refresh_hash ON public.proxy_sessions USING btree (refresh_hash);


--
-- Name: idx_proxy_sessions_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_proxy_sessions_user_id ON public.proxy_sessions USING btree (user_id);


--
-- Name: idx_resource_histories_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_resource_histories_created_at ON public.resource_histories USING btree (created_at DESC);


--
-- Name: idx_resource_histories_lookup_with_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_resource_histories_lookup_with_time ON public.resource_histories USING btree (cluster_name, resource_type, resource_name, namespace, created_at DESC);


--
-- Name: idx_resource_histories_operation_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_resource_histories_operation_source ON public.resource_histories USING btree (operation_source);


--
-- Name: idx_resource_histories_operation_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_resource_histories_operation_type ON public.resource_histories USING btree (operation_type);


--
-- Name: idx_resource_histories_operator_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_resource_histories_operator_id ON public.resource_histories USING btree (operator_id);


--
-- Name: idx_resource_templates_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_resource_templates_name ON public.resource_templates USING btree (name);


--
-- Name: idx_role_assignments_role_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_role_assignments_role_id ON public.role_assignments USING btree (role_id);


--
-- Name: idx_role_assignments_subject; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_role_assignments_subject ON public.role_assignments USING btree (subject, subject_type);


--
-- Name: idx_roles_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_roles_name ON public.roles USING btree (name);


--
-- Name: idx_user_groups_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_groups_name ON public.user_groups USING btree (name);


--
-- Name: idx_users_last_login_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_last_login_at ON public.users USING btree (last_login_at);


--
-- Name: idx_users_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_name ON public.users USING btree (name);


--
-- Name: idx_users_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_provider ON public.users USING btree (provider);


--
-- Name: idx_users_sub; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_sub ON public.users USING btree (sub);


--
-- Name: idx_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (username);


--
-- Name: resource_histories fk_resource_histories_operator; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.resource_histories
    ADD CONSTRAINT fk_resource_histories_operator FOREIGN KEY (operator_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: role_assignments fk_roles_assignments; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_assignments
    ADD CONSTRAINT fk_roles_assignments FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: user_group_members fk_user_group_members_user; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_group_members
    ADD CONSTRAINT fk_user_group_members_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: user_group_members fk_user_group_members_user_group; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_group_members
    ADD CONSTRAINT fk_user_group_members_user_group FOREIGN KEY (user_group_id) REFERENCES public.user_groups(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


