package unity

// commonStrings is Unity's built-in type-tree string table, by offset.
var commonStrings = func() map[uint32]string {
	list := []string{"AABB", "AnimationClip", "AnimationCurve", "AnimationState", "Array", "Base", "BitField", "bitset", "bool",
		"char", "ColorRGBA", "Component", "data", "deque", "double", "dynamic_array", "FastPropertyName", "first", "float",
		"Font", "GameObject", "Generic Mono", "GradientNEW", "GUID", "GUIStyle", "int", "list", "long long", "map",
		"Matrix4x4f", "MdFour", "MonoBehaviour", "MonoScript", "m_ByteSize", "m_Curve", "m_EditorClassIdentifier",
		"m_EditorHideFlags", "m_Enabled", "m_ExtensionPtr", "m_GameObject", "m_Index", "m_IsArray", "m_IsStatic",
		"m_MetaFlag", "m_Name", "m_ObjectHideFlags", "m_PrefabInternal", "m_PrefabParentObject", "m_Script",
		"m_StaticEditorFlags", "m_Type", "m_Version", "Object", "pair", "PPtr<Component>", "PPtr<GameObject>",
		"PPtr<Material>", "PPtr<MonoBehaviour>", "PPtr<MonoScript>", "PPtr<Object>", "PPtr<Prefab>", "PPtr<Sprite>",
		"PPtr<TextAsset>", "PPtr<Texture>", "PPtr<Texture2D>", "PPtr<Transform>", "Prefab", "Quaternionf", "Rectf",
		"RectInt", "RectOffset", "second", "set", "short", "size", "SInt16", "SInt32", "SInt64", "SInt8", "staticvector",
		"string", "TextAsset", "TextMesh", "Texture", "Texture2D", "Transform", "TypelessData", "UInt16", "UInt32",
		"UInt64", "UInt8", "unsigned int", "unsigned long long", "unsigned short", "vector", "Vector2f", "Vector3f",
		"Vector4f", "m_ScriptingClassIdentifier", "Gradient", "Type*", "int2_storage", "int3_storage", "BoundsInt",
		"m_CorrespondingSourceObject", "m_PrefabInstance", "m_PrefabAsset", "FileSize", "Hash128"}
	m := map[uint32]string{}
	off := uint32(0)
	for _, s := range list {
		m[off] = s
		off += uint32(len(s)) + 1
	}
	return m
}()
