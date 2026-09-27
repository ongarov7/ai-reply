-- 0004_ai_limits: көшірілген хабарламаның шегі енді әкімші панелінен басқарылады.
--
-- Бастапқы мән — 400 таңба (бұрын ортадағы LIMIT_SOURCE_TEXT_CHARS=300 еді).
-- INSERT OR IGNORE: әкімші мәнді бұрын қойған болса, ол өзгеріссіз қалады.
-- Нұсқау шегі мен max_output_tokens әкімші оларды сақтағанша ортадан оқылады.
INSERT OR IGNORE INTO system_settings (key, value, updated_at)
VALUES ('max_source_characters', '400', 0);
