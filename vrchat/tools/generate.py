#!/usr/bin/env python3
"""Generate named JSON operations and models from a pinned VRChat specification."""
import argparse
import functools
import json
from pathlib import Path
import re
import subprocess

import yaml

SPEC_REVISION = "3656fce60438134a94f9cb9fff98608aacb250c9"
HTTP_METHODS = {"get", "post", "put", "delete", "patch"}
INITIALISMS = {
    x.lower(): x
    for x in [
        "ID",
        "URL",
        "API",
        "IP",
        "JSON",
        "TOS",
        "SDK",
        "OTP",
        "TOTP",
        "HTTP",
        "HTTPS",
        "CPU",
        "GPU",
        "XML",
        "UTC",
        "IDs",
        "URLs",
        "FA",
    ]
}
RESERVED = {
    "Client",
    "Config",
    "Request",
    "Tokens",
    "Pipeline",
    "APIError",
    "Call",
    "Operation",
}


def name(value):
    value = value.replace("URLs", "Urls").replace("IDs", "Ids")
    words = re.findall(
        r"[A-Z]+(?=[A-Z][a-z]|[0-9]|[^A-Za-z]|$)|[A-Z]?[a-z]+|[0-9]+", value
    )
    result = "".join(
        INITIALISMS.get(
            w.lower(), (w.capitalize() if w.isupper() else w[:1].upper() + w[1:])
        )
        for w in words
    )
    return ("Value" if result[:1].isdigit() else "") + result


def variable(value):
    result = name(value)
    for initialism in sorted(INITIALISMS.values(), key=len, reverse=True):
        if result.startswith(initialism):
            result = initialism.lower() + result[len(initialism) :]
            break
    else:
        result = result[:1].lower() + result[1:]
    return (
        result + "Value"
        if result
        in {
            "type",
            "range",
            "map",
            "func",
            "var",
            "const",
            "default",
            "select",
            "chan",
            "interface",
            "package",
            "defer",
            "go",
            "return",
            "switch",
            "case",
            "break",
            "continue",
            "fallthrough",
        }
        else result
    )


@functools.lru_cache(maxsize=None)
def document(path):
    return yaml.safe_load(path.read_text())


def dereference(schema, base):
    seen = set()
    while "$ref" in schema:
        ref = schema["$ref"]
        key = (base, ref)
        if key in seen:
            raise ValueError(f"Circular direct reference: {key}")
        seen.add(key)
        file, _, fragment = ref.partition("#")
        base = (base.parent / file).resolve() if file else base
        schema = document(base)
        for part in fragment.lstrip("/").split("/") if fragment else []:
            schema = schema[part.replace("~1", "/").replace("~0", "~")]
    return schema, base


def schema_type(schema):
    typ = schema.get("type")
    if typ is None and schema.get("enum"):
        values = [v for v in schema["enum"] if v is not None]
        if values and all(isinstance(v, str) for v in values):
            typ = "string"
        elif values and all(isinstance(v, bool) for v in values):
            typ = "boolean"
        elif values and all(isinstance(v, int) for v in values):
            typ = "integer"
    if typ is None and isinstance(schema.get("const"), str):
        typ = "string"
    return typ


class Generator:
    def __init__(self):
        self.models = {}
        self.origins = {}

    def add(self, model, schema, base):
        model = name(model)
        if model in RESERVED:
            model = "API" + model
        origin = (base, json.dumps(schema, sort_keys=True, default=str))
        if model in self.origins and self.origins[model] != origin:
            # Identical inline schemas can have several references.
            if self.origins[model][1] != origin[1]:
                raise ValueError(f"Conflicting model name: {model}")
        self.origins[model] = origin
        self.models[model] = (schema, base)
        return model

    def properties(self, schema, base, seen=frozenset()):
        if "$ref" in schema:
            marker = (base, schema["$ref"])
            if marker in seen:
                return None
            schema, base = dereference(schema, base)
            return self.properties(schema, base, seen | {marker})
        if schema.get("type") == "null":
            return {}
        own = {
            key: (value, base) for key, value in schema.get("properties", {}).items()
        }
        if ("properties" in schema or schema.get("type") == "object") and not any(
            key in schema for key in ["allOf", "oneOf", "anyOf"]
        ):
            return own
        for union in ["allOf", "oneOf", "anyOf"]:
            if union in schema:
                out = dict(own)
                for child in schema[union]:
                    props = self.properties(child, base, seen)
                    if props is None:
                        return None
                    for key, value in props.items():
                        if key in out and json.dumps(
                            out[key][0], sort_keys=True, default=str
                        ) != json.dumps(value[0], sort_keys=True, default=str):
                            # Discriminated object variants share a single field.
                            # Preserve any incompatible field losslessly.
                            old, oldbase = dereference(out[key][0], out[key][1])
                            new, newbase = dereference(value[0], value[1])
                            if schema_type(old) == schema_type(new) and schema_type(
                                old
                            ) in ["string", "boolean", "integer", "number"]:
                                value = ({"type": schema_type(old)}, base)
                            else:
                                value = ({}, base)
                        out[key] = value
                return out
        return None

    def go_type(self, schema, base, hint):
        if not isinstance(schema, dict):
            return "json.RawMessage"
        if "$ref" in schema:
            resolved, target = dereference(schema, base)
            file, _, fragment = schema["$ref"].partition("#")
            title = target.stem + (name(fragment.replace("/", " ")) if fragment else "")
            # Fragment-only parameter refs are resolved before reaching here.
            return self.add(title, resolved, target)
        typ = schema_type(schema)
        if isinstance(typ, list):
            choices = [v for v in typ if v != "null"]
            if len(choices) == 1:
                sub = dict(schema, type=choices[0])
                return "*" + self.go_type(sub, base, hint)
            return "json.RawMessage"
        if typ == "array":
            return "[]" + self.go_type(schema.get("items", {}), base, hint + "Item")
        if typ in ["string", "integer", "number", "boolean"]:
            return {
                "string": "string",
                "integer": "int",
                "number": "float64",
                "boolean": "bool",
            }[typ]
        if "properties" in schema or any(
            k in schema for k in ["allOf", "oneOf", "anyOf"]
        ):
            return self.add(
                hint if hint.startswith("Inline") else "Inline" + hint, schema, base
            )
        if typ == "object" or "additionalProperties" in schema:
            additional = schema.get("additionalProperties", {})
            return "map[string]" + self.go_type(
                additional if isinstance(additional, dict) else {}, base, hint + "Value"
            )
        return "json.RawMessage"

    def definition(self, model, schema, base):
        props = self.properties(schema, base)
        # Empty objects are dictionaries rather than fieldless structs.
        if props:
            fields = []
            required = set(schema.get("required", []))
            for key, (prop, owner) in props.items():
                typ = self.go_type(prop, owner, model + name(key))
                if key not in required and not typ.startswith("*"):
                    typ = "*" + typ
                tag = key + (",omitempty" if key not in required else "")
                fields.append(f'\t{name(key)} {typ} `json:"{tag}"`')
            return f"type {model} struct {{\n" + "\n".join(fields) + "\n}\n"
        typ = self.go_type(
            {
                k: v
                for k, v in schema.items()
                if k not in ["title", "allOf", "oneOf", "anyOf", "properties"]
            },
            base,
            model + "Value",
        )
        if typ == model:
            raise ValueError(f"Self alias: {model}")
        out = f'type {model} {"= " if typ == "json.RawMessage" or typ.startswith("*") else ""}{typ}\n'
        if typ in ["string", "int"] and schema.get("enum"):
            constants = []
            used = set()
            for value in schema["enum"]:
                if value is None:
                    continue
                suffix = name(str(value)) or "Empty"
                key = model + suffix
                if key in used:
                    continue
                used.add(key)
                constants.append(f"\t{key} {model} = {json.dumps(value)}")
            out += "\nconst (\n" + "\n".join(constants) + "\n)\n"
        return out


def generate_operations(root, gen):
    methods = []
    coverage = []
    skipped = []
    names = set()
    model_names = {
        name(p.stem)
        for subdir in ["schemas", "requests"]
        for p in (root.parent / subdir).glob("*.yaml")
    }
    for pathfile in sorted(root.glob("*.yaml")):
        for path, item in document(pathfile)["paths"].items():
            for verb, op in item.items():
                if verb not in HTTP_METHODS:
                    continue
                op_name = name(op["operationId"])
                constructor_name = (
                    op_name + "Call" if op_name in model_names else op_name
                )
                if op_name in names:
                    raise ValueError("Duplicate operation " + op_name)
                names.add(op_name)
                if op_name == "DownloadFileVersion":
                    coverage.append(
                        {
                            "operation": op_name,
                            "method": verb.upper(),
                            "path": path,
                            "category": pathfile.stem,
                            "implementation": "assets.go",
                        }
                    )
                    continue
                request, requestbase = dereference(op.get("requestBody", {}), pathfile)
                content = request.get("content", {})
                if content and "application/json" not in content:
                    skipped.append(
                        {
                            "operation": op_name,
                            "reason": "Non-JSON request body",
                            "method": verb.upper(),
                            "path": path,
                        }
                    )
                    continue
                responses = op.get("responses", {})
                success = [
                    (str(code), response)
                    for code, response in responses.items()
                    if str(code).startswith("2")
                ]
                # Prefer the ordinary body-bearing success response.
                success.sort(key=lambda x: x[0])
                response = {}
                responsebase = pathfile
                for code, candidate in success:
                    candidate, owner = dereference(candidate, pathfile)
                    if candidate.get("content"):
                        response, responsebase = candidate, owner
                        break
                response_content = response.get("content", {})
                if response_content and "application/json" not in response_content:
                    skipped.append(
                        {
                            "operation": op_name,
                            "reason": "Non-JSON response body",
                            "method": verb.upper(),
                            "path": path,
                        }
                    )
                    continue
                schema = response_content.get("application/json", {}).get("schema", {})
                response_type = (
                    gen.go_type(schema, responsebase, op_name + "Response")
                    if schema
                    else "json.RawMessage"
                )
                factory = (
                    "newOperation" if schema else "newOperationAllowingEmptyResponse"
                )
                parameters = []
                optional = []
                param_fields = []
                path_pairs = []
                for param in item.get("parameters", []) + op.get("parameters", []):
                    param, base = dereference(param, pathfile)
                    key = param["name"]
                    where = param["in"]
                    prop = param.get("schema", {})
                    resolved, _ = dereference(prop, base)
                    if where == "path":
                        typ = "int" if resolved.get("type") == "integer" else "string"
                        arg = variable(key)
                        parameters.append(f"{arg} {typ}")
                        path_pairs.append(f"{{{json.dumps(key)}, fmt.Sprint({arg})}}")
                    elif where in ["query", "header"]:
                        typ = gen.go_type(prop, base, op_name + name(key))
                        if resolved.get("type") in [
                            "string",
                            "integer",
                            "number",
                            "boolean",
                        ]:
                            typ = {
                                "string": "string",
                                "integer": "int",
                                "number": "float64",
                                "boolean": "bool",
                            }[resolved["type"]]
                        # Query enums use named values but scalar presence is explicit.
                        if resolved.get("type") != "array" and not typ.startswith("*"):
                            typ = "*" + typ
                        explode = param.get("explode", True)
                        tags = f'{where}:"{key}"'
                        if not explode:
                            tags += ' explode:"false"'
                        if param.get("required", False):
                            tags += ' required:"true"'
                        param_fields.append(f"\t{name(key)} {typ} `{tags}`")
                    else:
                        raise ValueError("Unsupported parameter location " + where)
                if param_fields:
                    optional.append("params " + op_name + "Params")
                    methods.append(
                        f"// {op_name}Params contains query and header parameters.\ntype {op_name}Params struct {{\n"
                        + "\n".join(param_fields)
                        + "\n}\n"
                    )
                body = "nil"
                if "application/json" in content:
                    schema = content["application/json"]["schema"]
                    typ = gen.go_type(schema, requestbase, op_name + "Body")
                    if not request.get("required", False):
                        typ = "*" + typ
                    optional.append("body " + typ)
                    body = "body"
                signature = ", ".join(parameters + optional)
                arguments = ", ".join(
                    [x.rsplit(" ", 1)[0] for x in parameters + optional]
                )
                params = "params" if param_fields else "nil"
                paths = (
                    "[]pathParameter{" + ", ".join(path_pairs) + "}"
                    if path_pairs
                    else "nil"
                )
                summary = op.get("summary", op_name).rstrip(".")
                methods.append(
                    f"// {op_name} builds the request for {verb.upper()} {path}.\nfunc {constructor_name}({signature}) Operation[{response_type}] {{\n\treturn {factory}[{response_type}]({json.dumps(verb.upper())}, {json.dumps(path)}, {paths}, {params}, {body})\n}}\n"
                )
                # The existing logout helper already has this method name.
                if op_name != "Logout":
                    more = ", " + signature if signature else ""
                    methods.append(
                        f"// {op_name}: {summary}.\nfunc (c *Client) {op_name}(ctx context.Context, tokens Tokens{more}) ({response_type}, Tokens, error) {{\n\treturn Execute(ctx, c, tokens, {constructor_name}({arguments}))\n}}\n"
                    )
                coverage.append(
                    {
                        "operation": op_name,
                        "method": verb.upper(),
                        "path": path,
                        "category": pathfile.stem,
                    }
                )
    return methods, coverage, skipped


def render_models(gen):
    rendered = {}
    while set(gen.models) - set(rendered):
        for model in sorted(set(gen.models) - set(rendered)):
            schema, base = gen.models[model]
            rendered[model] = (
                ""
                if model == "SearchAvatarsResult"
                else gen.definition(model, schema, base)
            )
    return rendered


def write_files(output, methods, rendered, coverage, skipped):
    header = f"// Code generated by tools/generate.py from vrchatapi/specification {SPEC_REVISION}; DO NOT EDIT.\n// Upstream schema license: LICENSE.specification.\n\npackage vrchat\n\n"
    output.mkdir(parents=True, exist_ok=True)
    (output / "endpoints_generated.go").write_text(
        header + 'import ("context"; "encoding/json"; "fmt")\n\n' + "\n".join(methods)
    )
    (output / "models_generated.go").write_text(
        header
        + 'import "encoding/json"\n\n'
        + "\n".join(rendered[k] for k in sorted(rendered))
    )
    (output / "coverage.json").write_text(
        json.dumps(
            {
                "specification": "https://github.com/vrchatapi/specification",
                "revision": SPEC_REVISION,
                "operations": coverage,
                "excluded": skipped,
            },
            indent=2,
        )
        + "\n"
    )
    subprocess.run(
        [
            "gofmt",
            "-w",
            str(output / "endpoints_generated.go"),
            str(output / "models_generated.go"),
        ],
        check=True,
    )
    print(
        f"{len(coverage)} wrapped operations, {len(rendered)} models, {len(skipped)} non-JSON operations excluded"
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec-dir", type=Path, required=True)
    parser.add_argument(
        "--output", type=Path, default=Path(__file__).resolve().parents[1]
    )
    args = parser.parse_args()
    revision = subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=args.spec_dir, text=True
    ).strip()
    if revision != SPEC_REVISION:
        parser.error(f"Specification must be checked out at {SPEC_REVISION}")
    if subprocess.run(
        ["git", "diff", "--quiet", "HEAD", "--", "openapi"], cwd=args.spec_dir
    ).returncode:
        parser.error("The pinned specification has uncommitted changes")
    root = args.spec_dir.resolve() / "openapi/components/paths"
    gen = Generator()
    methods, coverage, skipped = generate_operations(root, gen)
    rendered = render_models(gen)
    write_files(args.output, methods, rendered, coverage, skipped)


if __name__ == "__main__":
    main()
