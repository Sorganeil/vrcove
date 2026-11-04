import unittest
from pathlib import Path

from generate import Generator, name


class SchemaContracts(unittest.TestCase):
    def setUp(self):
        self.generator = Generator()
        self.base = Path("/spec/requests/UpdateProfileRequest.yaml")

    def test_go_names_preserve_words_and_initialisms(self):
        for source, expected in {
            "MAX_ROLES": "MaxRoles",
            "MAX_MANAGEMENT_ROLES": "MaxManagementRoles",
            "userId": "UserID",
            "roleIds": "RoleIDs",
            "assetURLs": "AssetURLs",
            "verify2FA": "Verify2FA",
        }.items():
            self.assertEqual(name(source), expected)

    def test_patch_fields_preserve_false_empty_string_and_empty_array(self):
        schema = {
            "type": "object",
            "properties": {
                "bio": {"type": "string"},
                "enabled": {"type": "boolean"},
                "bioLinks": {"type": "array", "items": {"type": "string"}},
            },
        }
        code = self.generator.definition("Update", schema, self.base)
        self.assertIn('Bio *string `json:"bio,omitempty"`', code)
        self.assertIn('Enabled *bool `json:"enabled,omitempty"`', code)
        self.assertIn('BioLinks *[]string `json:"bioLinks,omitempty"`', code)

    def test_required_zero_values_are_not_omitted(self):
        schema = {
            "type": "object",
            "properties": {"enabled": {"type": "boolean"}},
            "required": ["enabled"],
        }
        code = self.generator.definition("Create", schema, self.base)
        self.assertIn('Enabled bool `json:"enabled"`', code)

    def test_inherited_and_local_properties_survive(self):
        schema = {
            "type": "object",
            "properties": {"name": {"type": "string"}},
            "allOf": [{"type": "object", "properties": {"id": {"type": "string"}}}],
        }
        code = self.generator.definition("Member", schema, self.base)
        self.assertIn("Name *string", code)
        self.assertIn("ID *string", code)

    def test_enum_without_type_is_a_string(self):
        code = self.generator.definition("Region", {"enum": ["us", "eu"]}, self.base)
        self.assertIn("type Region string", code)
        self.assertIn('RegionEu Region = "eu"', code)

    def test_unrepresentable_union_retains_raw_json_decoder(self):
        code = self.generator.definition(
            "Value", {"oneOf": [{"type": "string"}, {"type": "boolean"}]}, self.base
        )
        self.assertIn("type Value = json.RawMessage", code)

    def test_boolean_enum_does_not_become_an_integer(self):
        code = self.generator.definition("Flag", {"enum": [True, False]}, self.base)
        self.assertIn("type Flag bool", code)

    def test_discriminator_constants_share_a_typed_string(self):
        schema = {
            "oneOf": [
                {"type": "object", "properties": {"type": {"const": "friendRequest"}}},
                {"type": "object", "properties": {"type": {"const": "invite"}}},
            ]
        }
        code = self.generator.definition("Notification", schema, self.base)
        self.assertIn('Type *string `json:"type,omitempty"`', code)

    def test_impossible_tuple_tail_remains_lossless(self):
        self.assertEqual(
            self.generator.go_type(False, self.base, "TupleTail"), "json.RawMessage"
        )


if __name__ == "__main__":
    unittest.main()
