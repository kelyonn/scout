-- Bootstrap data for a fresh database: the single user (ADR-015), their real
-- profile (skills/target roles pulled from the resume, not placeholder
-- defaults), and the active weight_version scoring uses. Idempotent — safe
-- to run more than once against the same database.
--
-- Not a migration on purpose: an email address is the operator's personal
-- data, and a migration is a permanent, committed artifact. This runs via
-- `make seed`, with the email passed as a psql variable rather than baked
-- into a tracked file. See infra/scripts/seed.sh.

INSERT INTO app_user (email, display_name, timezone, locale)
VALUES (:'user_email', 'Scout', 'Asia/Kolkata', 'en-IN')
ON CONFLICT (email) DO NOTHING;

-- Real profile, not defaults: skills/skill_levels are pulled from the
-- resume (packages/taxonomy/skills.yaml carries the matching ontology
-- entries — extended alongside this change for the systems/ML/security
-- terms the base ontology lacked). target_roles is the full swe.* set per
-- the user's explicit "cast the widest net" instruction — advocacy.* is
-- left out since nothing here is a devrel/solutions fit. target_seniority
-- keeps the table default ({internship,new_grad}) since graduation is 2027.
-- location_tiers.4.multiplier is raised 0.90 -> 1.00 here for documentation
-- parity with weight_version below, but note scoring.go's
-- locationMultiplierFor reads weight_version.weights.location_multipliers,
-- not this column — the functional fix is the weight_version UPDATE below.
INSERT INTO user_profile (
    user_id, graduation_year, degree, university,
    target_roles, target_seniority, skills, skill_levels, location_tiers
)
SELECT
    id,
    2027,
    'B.Tech Computer Science',
    'PES University, Bengaluru',
    ARRAY[
        'swe.general', 'swe.backend', 'swe.frontend', 'swe.fullstack', 'swe.mobile',
        'swe.ml', 'swe.ml.research', 'swe.data', 'swe.infra', 'swe.infra.sre',
        'swe.infra.devops', 'swe.infra.platform', 'swe.infra.cloud', 'swe.systems',
        'swe.security', 'swe.embedded', 'swe.qa', 'swe.research', 'swe.other'
    ]::role_family[],
    '{internship,new_grad}'::seniority[],
    ARRAY[
        'go', 'python', 'c', 'javascript', 'typescript', 'swift', 'sql',
        'fastapi', 'nodejs', 'react', 'webgl', 'tailwind', 'supabase', 'mqtt',
        'websockets', 'timescaledb', 'prometheus', 'grafana', 'argocd', 'chaos_engineering',
        'consensus_algorithms', 'linux_namespaces', 'cgroups', 'bpf_seccomp', 'container_runtimes',
        'merkle_trees', 'cryptography', 'blockchain', 'zero_trust',
        'docker', 'kubernetes',
        'computer_vision', 'nlp', 'graph_neural_networks', 'llm_inference', 'genetic_algorithms',
        'machine_learning', 'pytorch', 'tensorflow',
        'git', 'data_structures', 'algorithms', 'system_design', 'testing', 'rest_api', 'oop'
    ],
    '{
        "go": 5, "python": 4, "c": 3, "javascript": 3, "typescript": 4, "swift": 3, "sql": 4,
        "fastapi": 4, "nodejs": 2, "react": 2, "webgl": 3, "tailwind": 3, "supabase": 3, "mqtt": 3,
        "websockets": 3, "timescaledb": 4, "prometheus": 4, "grafana": 4, "argocd": 4, "chaos_engineering": 4,
        "consensus_algorithms": 5, "linux_namespaces": 5, "cgroups": 5, "bpf_seccomp": 4, "container_runtimes": 5,
        "merkle_trees": 5, "cryptography": 4, "blockchain": 4, "zero_trust": 4,
        "docker": 4, "kubernetes": 4,
        "computer_vision": 4, "nlp": 3, "graph_neural_networks": 3, "llm_inference": 5, "genetic_algorithms": 4,
        "machine_learning": 4, "pytorch": 3, "tensorflow": 2,
        "git": 5, "data_structures": 4, "algorithms": 4, "system_design": 4, "testing": 4, "rest_api": 4, "oop": 4
    }'::jsonb,
    '{
        "1": {"cities": ["Bengaluru"], "multiplier": 1.20},
        "2": {"countries": ["IN"], "multiplier": 1.05},
        "3": {"remote": true, "multiplier": 1.12},
        "4": {"countries": ["US", "CA", "SG", "GB", "AU", "JP", "DE", "NL"], "multiplier": 1.00}
    }'::jsonb
FROM app_user WHERE email = :'user_email'
ON CONFLICT (user_id) DO UPDATE SET
    graduation_year = excluded.graduation_year,
    degree = excluded.degree,
    university = excluded.university,
    target_roles = excluded.target_roles,
    target_seniority = excluded.target_seniority,
    skills = excluded.skills,
    skill_levels = excluded.skill_levels,
    location_tiers = excluded.location_tiers,
    updated_at = now();

-- docs/09-ranking-scoring.md section 4's literal example. `version` and
-- `source` are separate columns already, so unlike the doc's own example
-- (which shows the whole row as one JSON blob) the `weights` column here
-- holds only the weights object itself, not a redundant copy of the two
-- columns sitting next to it. apps/collector/internal/scoring reads this at
-- runtime rather than hardcoding it.
INSERT INTO weight_version (version, weights, source, active)
VALUES (
    'v1-hand-tuned-2026-08',
    '{
        "priority": {
            "overall_match": 0.24,
            "company_quality": 0.14,
            "learning_opportunity": 0.12,
            "interview_probability": 0.10,
            "competition_estimate": 0.10,
            "compensation": 0.09,
            "growth_potential": 0.07,
            "engineering_culture": 0.06,
            "ease_of_applying": 0.05,
            "deadline_urgency": 0.03
        },
        "overall_match": {
            "skill_match": 0.40,
            "resume_match": 0.30,
            "role_family_fit": 0.20,
            "seniority_fit": 0.10
        },
        "location_multipliers": {"1": 1.20, "2": 1.05, "3": 1.12, "4": 1.00},
        "freshness_half_life_days": 14,
        "freshness_floor": 0.55
    }'::jsonb,
    'hand_tuned',
    TRUE
)
ON CONFLICT (version) DO UPDATE SET weights = excluded.weights;
