namespace AirReply.Core.Generation;

/// <summary>Who a reply is for. Wire values are the server's built-in templates: friend, client, work, business.</summary>
public enum Recipient { Friend, Client, Work, Business }

public static class RecipientWire
{
    public static string ToWire(this Recipient recipient) => recipient switch
    {
        Recipient.Friend => "friend",
        Recipient.Work => "work",
        Recipient.Business => "business",
        _ => "client",
    };

    public static Recipient? FromWire(string? value) => value switch
    {
        "friend" => Recipient.Friend,
        "client" => Recipient.Client,
        "work" or "colleague" => Recipient.Work,
        "business" or "partner" => Recipient.Business,
        _ => null,
    };
}

/// <summary>
/// The desktop context of one request, already bounded by the client to the server's limits:
/// profile ≤ 1500 characters, ≤ 8 references, ≤ 4000 reference characters, ≤ 4096 UTF-8 bytes in total.
/// Built by AirReply.Context; sent as the request's "context" object.
/// </summary>
public sealed record GenerationContextPayload(string? Profile, IReadOnlyList<string> References);

/// <summary>
/// Optional desktop fields. Each is sent only when the user actually chose it (audit MAC-1): no context, no
/// output limit and no template for an ordinary request. Valid output limits: 400 (Standard) and 600 (Extended).
/// </summary>
public sealed record GenerationOptions(GenerationContextPayload? Context = null, int? OutputCharacterLimit = null)
{
    public static readonly GenerationOptions None = new();

    public bool IsEmpty => Context is null && OutputCharacterLimit is null;
}
