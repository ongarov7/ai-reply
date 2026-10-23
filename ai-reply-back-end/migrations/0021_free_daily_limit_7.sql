-- The owner restores the canonical free plan to seven daily replies on upgrade.
-- Plans are editable data: changing the original seed cannot repair an existing DB.
-- Run once through schema_migrations; later administrator edits remain authoritative.
-- Only the known old boilerplate is synchronized. Custom descriptions (including
-- text which happens to contain 20), names, paid plans, usage and credits stay intact.
UPDATE plans
SET daily_message_limit = 7,
    description_kk = CASE WHEN description_kk = 'Күніне 20 жауап. Картасыз бастаңыз.'
                          THEN 'Күніне 7 жауап. Картасыз бастаңыз.' ELSE description_kk END,
    description_ru = CASE WHEN description_ru = '20 ответов в день. Начните без карты.'
                          THEN '7 ответов в день. Начните без карты.' ELSE description_ru END,
    description_en = CASE WHEN description_en = '20 replies per day. Start without a card.'
                          THEN '7 replies per day. Start without a card.' ELSE description_en END,
    description_uz = CASE WHEN description_uz = 'Kuniga 20 ta javob. Kartasiz boshlang.'
                          THEN 'Kuniga 7 ta javob. Kartasiz boshlang.' ELSE description_uz END,
    updated_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE code = 'free' AND is_free = 1
  AND (daily_message_limit <> 7
       OR description_kk = 'Күніне 20 жауап. Картасыз бастаңыз.'
       OR description_ru = '20 ответов в день. Начните без карты.'
       OR description_en = '20 replies per day. Start without a card.'
       OR description_uz = 'Kuniga 20 ta javob. Kartasiz boshlang.');
