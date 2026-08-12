package conformance

import (
	"testing"

	ucp "github.com/chaz8081/ucp-go"
	"github.com/chaz8081/ucp-go/shopping"
	"github.com/chaz8081/ucp-go/shopping/types"
	"github.com/chaz8081/ucp-go/transports"
)

// validator is the uniform interface every generated type satisfies.
type validator interface{ Validate() error }

// models maps a schema to a fresh value of the Go type it produces.
//
// Go cannot construct a type from a name, so driving the corpus by schema
// path needs an explicit table. It is generated from the emitter's type
// index rather than hand-written, and TestModelsCoverCorpus below fails if
// it ever falls behind the corpus.
//
// Only schemas that emit a file-level type appear: the rest — a bare enum,
// a union with no properties of its own — produce no value to construct.
var models = map[string]func() validator{
	"shopping/cart.json":                                              func() validator { return new(shopping.Cart) },
	"shopping/cart_create_request.json":                               func() validator { return new(shopping.CartCreateRequest) },
	"shopping/cart_update_request.json":                               func() validator { return new(shopping.CartUpdateRequest) },
	"shopping/catalog_lookup.json":                                    func() validator { return new(shopping.CatalogLookup) },
	"shopping/catalog_search.json":                                    func() validator { return new(shopping.CatalogSearch) },
	"shopping/checkout.json":                                          func() validator { return new(shopping.Checkout) },
	"shopping/checkout_complete_request.json":                         func() validator { return new(shopping.CheckoutCompleteRequest) },
	"shopping/checkout_create_request.json":                           func() validator { return new(shopping.CheckoutCreateRequest) },
	"shopping/checkout_update_request.json":                           func() validator { return new(shopping.CheckoutUpdateRequest) },
	"shopping/order.json":                                             func() validator { return new(shopping.Order) },
	"shopping/order_create_request.json":                              func() validator { return new(shopping.OrderCreateRequest) },
	"shopping/order_update_request.json":                              func() validator { return new(shopping.OrderUpdateRequest) },
	"shopping/payment.json":                                           func() validator { return new(shopping.Payment) },
	"shopping/payment_complete_request.json":                          func() validator { return new(shopping.PaymentCompleteRequest) },
	"shopping/payment_create_request.json":                            func() validator { return new(shopping.PaymentCreateRequest) },
	"shopping/payment_update_request.json":                            func() validator { return new(shopping.PaymentUpdateRequest) },
	"shopping/types/account_info.json":                                func() validator { return new(types.PaymentAccountInfo) },
	"shopping/types/adjustment.json":                                  func() validator { return new(types.Adjustment) },
	"shopping/types/adjustment_create_request.json":                   func() validator { return new(types.AdjustmentCreateRequest) },
	"shopping/types/adjustment_update_request.json":                   func() validator { return new(types.AdjustmentUpdateRequest) },
	"shopping/types/amount.json":                                      func() validator { return new(types.Amount) },
	"shopping/types/attribution.json":                                 func() validator { return new(types.Attribution) },
	"shopping/types/attribution_complete_request.json":                func() validator { return new(types.AttributionCompleteRequest) },
	"shopping/types/attribution_create_request.json":                  func() validator { return new(types.AttributionCreateRequest) },
	"shopping/types/attribution_update_request.json":                  func() validator { return new(types.AttributionUpdateRequest) },
	"shopping/types/available_payment_instrument.json":                func() validator { return new(types.AvailablePaymentInstrument) },
	"shopping/types/binding.json":                                     func() validator { return new(types.Binding) },
	"shopping/types/business_fulfillment_config.json":                 func() validator { return new(types.BusinessFulfillmentConfig) },
	"shopping/types/buyer.json":                                       func() validator { return new(types.Buyer) },
	"shopping/types/buyer_create_request.json":                        func() validator { return new(types.BuyerCreateRequest) },
	"shopping/types/buyer_update_request.json":                        func() validator { return new(types.BuyerUpdateRequest) },
	"shopping/types/card_credential.json":                             func() validator { return new(types.CardCredential) },
	"shopping/types/card_payment_instrument.json":                     func() validator { return new(types.CardPaymentInstrument) },
	"shopping/types/category.json":                                    func() validator { return new(types.Category) },
	"shopping/types/context.json":                                     func() validator { return new(types.Context) },
	"shopping/types/context_create_request.json":                      func() validator { return new(types.ContextCreateRequest) },
	"shopping/types/context_update_request.json":                      func() validator { return new(types.ContextUpdateRequest) },
	"shopping/types/description.json":                                 func() validator { return new(types.Description) },
	"shopping/types/detail_option_value.json":                         func() validator { return new(types.DetailOptionValue) },
	"shopping/types/error_code.json":                                  func() validator { return new(types.ErrorCode) },
	"shopping/types/error_response.json":                              func() validator { return new(types.ErrorResponse) },
	"shopping/types/expectation.json":                                 func() validator { return new(types.Expectation) },
	"shopping/types/expectation_create_request.json":                  func() validator { return new(types.ExpectationCreateRequest) },
	"shopping/types/expectation_update_request.json":                  func() validator { return new(types.ExpectationUpdateRequest) },
	"shopping/types/fulfillment.json":                                 func() validator { return new(types.Fulfillment) },
	"shopping/types/fulfillment_available_method.json":                func() validator { return new(types.FulfillmentAvailableMethod) },
	"shopping/types/fulfillment_available_method_create_request.json": func() validator { return new(types.FulfillmentAvailableMethodCreateRequest) },
	"shopping/types/fulfillment_available_method_update_request.json": func() validator { return new(types.FulfillmentAvailableMethodUpdateRequest) },
	"shopping/types/fulfillment_create_request.json":                  func() validator { return new(types.FulfillmentCreateRequest) },
	"shopping/types/fulfillment_destination.json":                     func() validator { return new(types.FulfillmentDestination) },
	"shopping/types/fulfillment_destination_create_request.json":      func() validator { return new(types.FulfillmentDestinationCreateRequest) },
	"shopping/types/fulfillment_destination_update_request.json":      func() validator { return new(types.FulfillmentDestinationUpdateRequest) },
	"shopping/types/fulfillment_event.json":                           func() validator { return new(types.FulfillmentEvent) },
	"shopping/types/fulfillment_event_create_request.json":            func() validator { return new(types.FulfillmentEventCreateRequest) },
	"shopping/types/fulfillment_event_update_request.json":            func() validator { return new(types.FulfillmentEventUpdateRequest) },
	"shopping/types/fulfillment_group.json":                           func() validator { return new(types.FulfillmentGroup) },
	"shopping/types/fulfillment_group_create_request.json":            func() validator { return new(types.FulfillmentGroupCreateRequest) },
	"shopping/types/fulfillment_group_update_request.json":            func() validator { return new(types.FulfillmentGroupUpdateRequest) },
	"shopping/types/fulfillment_method.json":                          func() validator { return new(types.FulfillmentMethod) },
	"shopping/types/fulfillment_method_create_request.json":           func() validator { return new(types.FulfillmentMethodCreateRequest) },
	"shopping/types/fulfillment_method_update_request.json":           func() validator { return new(types.FulfillmentMethodUpdateRequest) },
	"shopping/types/fulfillment_option.json":                          func() validator { return new(types.FulfillmentOption) },
	"shopping/types/fulfillment_option_create_request.json":           func() validator { return new(types.FulfillmentOptionCreateRequest) },
	"shopping/types/fulfillment_option_update_request.json":           func() validator { return new(types.FulfillmentOptionUpdateRequest) },
	"shopping/types/fulfillment_update_request.json":                  func() validator { return new(types.FulfillmentUpdateRequest) },
	"shopping/types/info_code.json":                                   func() validator { return new(types.InfoCode) },
	"shopping/types/input_correlation.json":                           func() validator { return new(types.InputCorrelation) },
	"shopping/types/item.json":                                        func() validator { return new(types.Item) },
	"shopping/types/item_create_request.json":                         func() validator { return new(types.ItemCreateRequest) },
	"shopping/types/item_update_request.json":                         func() validator { return new(types.ItemUpdateRequest) },
	"shopping/types/line_item.json":                                   func() validator { return new(types.LineItem) },
	"shopping/types/line_item_create_request.json":                    func() validator { return new(types.LineItemCreateRequest) },
	"shopping/types/line_item_update_request.json":                    func() validator { return new(types.LineItemUpdateRequest) },
	"shopping/types/link.json":                                        func() validator { return new(types.Link) },
	"shopping/types/media.json":                                       func() validator { return new(types.Media) },
	"shopping/types/merchant_fulfillment_config.json":                 func() validator { return new(types.MerchantFulfillmentConfig) },
	"shopping/types/message.json":                                     func() validator { return new(types.Message) },
	"shopping/types/message_create_request.json":                      func() validator { return new(types.MessageCreateRequest) },
	"shopping/types/message_error.json":                               func() validator { return new(types.MessageError) },
	"shopping/types/message_info.json":                                func() validator { return new(types.MessageInfo) },
	"shopping/types/message_update_request.json":                      func() validator { return new(types.MessageUpdateRequest) },
	"shopping/types/message_warning.json":                             func() validator { return new(types.MessageWarning) },
	"shopping/types/option_value.json":                                func() validator { return new(types.OptionValue) },
	"shopping/types/order_confirmation.json":                          func() validator { return new(types.OrderConfirmation) },
	"shopping/types/order_line_item.json":                             func() validator { return new(types.OrderLineItem) },
	"shopping/types/order_line_item_create_request.json":              func() validator { return new(types.OrderLineItemCreateRequest) },
	"shopping/types/order_line_item_update_request.json":              func() validator { return new(types.OrderLineItemUpdateRequest) },
	"shopping/types/pagination.json":                                  func() validator { return new(types.Pagination) },
	"shopping/types/payment_credential.json":                          func() validator { return new(types.PaymentCredential) },
	"shopping/types/payment_credential_complete_request.json":         func() validator { return new(types.PaymentCredentialCompleteRequest) },
	"shopping/types/payment_credential_create_request.json":           func() validator { return new(types.PaymentCredentialCreateRequest) },
	"shopping/types/payment_credential_update_request.json":           func() validator { return new(types.PaymentCredentialUpdateRequest) },
	"shopping/types/payment_identity.json":                            func() validator { return new(types.PaymentIdentity) },
	"shopping/types/payment_instrument.json":                          func() validator { return new(types.PaymentInstrument) },
	"shopping/types/payment_instrument_complete_request.json":         func() validator { return new(types.PaymentInstrumentCompleteRequest) },
	"shopping/types/payment_instrument_create_request.json":           func() validator { return new(types.PaymentInstrumentCreateRequest) },
	"shopping/types/payment_instrument_update_request.json":           func() validator { return new(types.PaymentInstrumentUpdateRequest) },
	"shopping/types/platform_fulfillment_config.json":                 func() validator { return new(types.PlatformFulfillmentConfig) },
	"shopping/types/postal_address.json":                              func() validator { return new(types.PostalAddress) },
	"shopping/types/postal_address_complete_request.json":             func() validator { return new(types.PostalAddressCompleteRequest) },
	"shopping/types/postal_address_create_request.json":               func() validator { return new(types.PostalAddressCreateRequest) },
	"shopping/types/postal_address_update_request.json":               func() validator { return new(types.PostalAddressUpdateRequest) },
	"shopping/types/price.json":                                       func() validator { return new(types.Price) },
	"shopping/types/price_filter.json":                                func() validator { return new(types.PriceFilter) },
	"shopping/types/price_range.json":                                 func() validator { return new(types.PriceRange) },
	"shopping/types/product.json":                                     func() validator { return new(types.Product) },
	"shopping/types/product_option.json":                              func() validator { return new(types.ProductOption) },
	"shopping/types/rating.json":                                      func() validator { return new(types.Rating) },
	"shopping/types/retail_location.json":                             func() validator { return new(types.RetailLocation) },
	"shopping/types/retail_location_create_request.json":              func() validator { return new(types.RetailLocationCreateRequest) },
	"shopping/types/retail_location_update_request.json":              func() validator { return new(types.RetailLocationUpdateRequest) },
	"shopping/types/reverse_domain_name.json":                         func() validator { return new(types.ReverseDomainName) },
	"shopping/types/reverse_domain_name_create_request.json":          func() validator { return new(types.ReverseDomainNameCreateRequest) },
	"shopping/types/reverse_domain_name_update_request.json":          func() validator { return new(types.ReverseDomainNameUpdateRequest) },
	"shopping/types/search_filters.json":                              func() validator { return new(types.SearchFilters) },
	"shopping/types/selected_option.json":                             func() validator { return new(types.SelectedOption) },
	"shopping/types/shipping_destination.json":                        func() validator { return new(types.ShippingDestination) },
	"shopping/types/shipping_destination_create_request.json":         func() validator { return new(types.ShippingDestinationCreateRequest) },
	"shopping/types/shipping_destination_update_request.json":         func() validator { return new(types.ShippingDestinationUpdateRequest) },
	"shopping/types/signals.json":                                     func() validator { return new(types.Signals) },
	"shopping/types/signals_complete_request.json":                    func() validator { return new(types.SignalsCompleteRequest) },
	"shopping/types/signals_create_request.json":                      func() validator { return new(types.SignalsCreateRequest) },
	"shopping/types/signals_update_request.json":                      func() validator { return new(types.SignalsUpdateRequest) },
	"shopping/types/signed_amount.json":                               func() validator { return new(types.SignedAmount) },
	"shopping/types/token_credential.json":                            func() validator { return new(types.TokenCredential) },
	"shopping/types/total.json":                                       func() validator { return new(types.Total) },
	"shopping/types/total_create_request.json":                        func() validator { return new(types.TotalCreateRequest) },
	"shopping/types/total_update_request.json":                        func() validator { return new(types.TotalUpdateRequest) },
	"shopping/types/totals.json":                                      func() validator { return new(types.Totals) },
	"shopping/types/totals_create_request.json":                       func() validator { return new(types.TotalsCreateRequest) },
	"shopping/types/totals_update_request.json":                       func() validator { return new(types.TotalsUpdateRequest) },
	"shopping/types/variant.json":                                     func() validator { return new(types.Variant) },
	"shopping/types/warning_code.json":                                func() validator { return new(types.WarningCode) },
	"transports/embedded_config.json":                                 func() validator { return new(transports.EmbeddedTransportConfig) },
	"ucp.json":                                                        func() validator { return new(ucp.UCPMetadata) },
	"ucp_create_request.json":                                         func() validator { return new(ucp.UCPMetadataCreateRequest) },
	"ucp_update_request.json":                                         func() validator { return new(ucp.UCPMetadataUpdateRequest) },
}

// TestModelsCoverCorpus keeps the table honest. Without it a schema added
// upstream would simply be skipped by the differential harness, quietly
// shrinking coverage.
func TestModelsCoverCorpus(t *testing.T) {
	set := loadGoldens(t)
	idx := buildIndex(t, set)
	var missing []string
	for _, rel := range sortedKeys(set) {
		if _, ok := idx.Lookup(rel, ""); !ok {
			continue // no file-level type, nothing to construct
		}
		if _, ok := models[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		t.Errorf("models is missing %d schemas that emit a file-level type: %v", len(missing), missing)
	}
}
