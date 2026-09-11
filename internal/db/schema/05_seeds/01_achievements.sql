INSERT INTO achievements (code, title, description, points, requirements, is_global_announcement)
VALUES (
    'gbl_slr_aprntc',
    'Goblin Slayer Apprentice',
    'Kill 10 goblins',
    10,
    '[
        {
            "group_id": 1,
            "req_type": "kill:goblin",
            "target_value": 10
        }
    ]'::jsonb,
    true
)
ON CONFLICT (code) DO NOTHING;

INSERT INTO achievements (code, title, description, points, requirements)
VALUES (
    'bgnr_slyr',
    'Beginner Slayer',
    'Kill 10 goblins OR 1 archer',
    20,
    '[
        {
            "group_id": 1,
            "req_type": "kill:goblin",
            "target_value": 10
        },
        {
            "group_id": 2,
            "req_type": "kill:archer",
            "target_value": 1
        }
    ]'::jsonb
)
ON CONFLICT (code) DO NOTHING;

INSERT INTO achievements (code, title, description, points, requirements)
VALUES (
    'pro_slyr',
    'Professional Slayer',
    'Kill 250 goblins OR 50 archers',
    50,
    '[
        {
            "group_id": 1,
            "req_type": "kill:goblin",
            "target_value": 250
        },
        {
            "group_id": 2,
            "req_type": "kill:archer",
            "target_value": 50
        }
    ]'::jsonb
)
ON CONFLICT (code) DO NOTHING;

INSERT INTO achievements (code, title, description, points, requirements)
VALUES (
    'spdy_gbl_slyr',
    'Speedy Goblin Slayer',
    'Complete level in under 60 seconds AND kill 3 goblins',
    100,
    '[
        {
            "group_id": 1,
            "req_type": "time:level_speedrun",
            "target_value": 60
        },
        {
            "group_id": 1,
            "req_type": "kill:goblin",
            "target_value": 3
        }
    ]'::jsonb
)
ON CONFLICT (code) DO NOTHING;
INSERT INTO achievements (code, title, description, points, requirements)
VALUES (
    'gm_gbl_slyr',
    'ACH_SLAYER_SAME_GAME',
    'KILL 3 Goblins in the same game',
    100,
    '[
        {
            "group_id": 1,
            "req_type": "game:default",
            "target_value": 0
        },
        {
            "group_id": 1,
            "req_type": "kill:goblin",
            "target_value": 3
        }
    ]'::jsonb
)
ON CONFLICT (code) DO NOTHING;