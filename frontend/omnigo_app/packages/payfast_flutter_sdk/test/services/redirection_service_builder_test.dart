import 'package:flutter_test/flutter_test.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

/// A builder with every required field populated. Individual tests clear or
/// override one field at a time.
RedirectionServiceBuilder validBuilder() {
  return RedirectionService.newBuilder()
      .setMerchantId('MID-123')
      .setSecuredKey('SECRET-abc')
      .setBasketID('BASKET-001')
      .setTxnAmount('1500.00')
      .setDescription('Order #001')
      .setCustomerEmail('customer@example.com')
      .setCustomerMobile('03001234567')
      .setMerchantName('My Store')
      .setSuccessURL('https://merchant.example/success')
      .setFailureURL('https://merchant.example/failure')
      .setCheckoutURL('https://merchant.example/checkout');
}

void main() {
  group('build', () {
    test('produces a service from a fully populated builder', () {
      final service = validBuilder().build();

      expect(service.merchantId, 'MID-123');
      expect(service.basketId, 'BASKET-001');
      expect(service.amount, '1500.00');
      expect(service.merchantName, 'My Store');
      expect(service.checkoutUrl, 'https://merchant.example/checkout');
    });

    test('defaults to UAT with an empty token', () {
      final service = validBuilder().build();

      expect(service.isUAT, isTrue);
      expect(service.token, isEmpty);
    });

    test('honours setIsUAT(false) and a pre-fetched token', () {
      final service =
          validBuilder().setIsUAT(false).setToken('tok_existing').build();

      expect(service.isUAT, isFalse);
      expect(service.token, 'tok_existing');
    });

    test('leaves listeners null when they are not supplied', () {
      final service = validBuilder().build();

      expect(service.successListener, isNull);
      expect(service.failureListener, isNull);
      expect(service.tokenErrorListener, isNull);
    });

    test('retains supplied listeners', () {
      void onSuccess(String url) {}
      void onFailure(String url) {}
      void onTokenError(String error) {}

      final service = validBuilder()
          .setSuccessListener(onSuccess)
          .setFailureListener(onFailure)
          .setTokenErrorListener(onTokenError)
          .build();

      expect(service.successListener, same(onSuccess));
      expect(service.failureListener, same(onFailure));
      expect(service.tokenErrorListener, same(onTokenError));
    });
  });

  group('build validation', () {
    // Each entry omits exactly one required field.
    final cases = <String, RedirectionServiceBuilder Function()>{
      'Basket ID': () => RedirectionService.newBuilder()
          .setMerchantId('MID-123')
          .setSecuredKey('SECRET-abc')
          .setTxnAmount('1500.00')
          .setDescription('Order')
          .setCustomerEmail('c@example.com')
          .setCustomerMobile('03001234567')
          .setMerchantName('Store')
          .setSuccessURL('https://merchant.example/success')
          .setFailureURL('https://merchant.example/failure')
          .setCheckoutURL('https://merchant.example/checkout'),
      'Amount': () => RedirectionService.newBuilder()
          .setMerchantId('MID-123')
          .setSecuredKey('SECRET-abc')
          .setBasketID('BASKET-001')
          .setDescription('Order')
          .setCustomerEmail('c@example.com')
          .setCustomerMobile('03001234567')
          .setMerchantName('Store')
          .setSuccessURL('https://merchant.example/success')
          .setFailureURL('https://merchant.example/failure')
          .setCheckoutURL('https://merchant.example/checkout'),
      'Merchant ID': () => RedirectionService.newBuilder()
          .setSecuredKey('SECRET-abc')
          .setBasketID('BASKET-001')
          .setTxnAmount('1500.00')
          .setDescription('Order')
          .setCustomerEmail('c@example.com')
          .setCustomerMobile('03001234567')
          .setMerchantName('Store')
          .setSuccessURL('https://merchant.example/success')
          .setFailureURL('https://merchant.example/failure')
          .setCheckoutURL('https://merchant.example/checkout'),
      'Secured key': () => RedirectionService.newBuilder()
          .setMerchantId('MID-123')
          .setBasketID('BASKET-001')
          .setTxnAmount('1500.00')
          .setDescription('Order')
          .setCustomerEmail('c@example.com')
          .setCustomerMobile('03001234567')
          .setMerchantName('Store')
          .setSuccessURL('https://merchant.example/success')
          .setFailureURL('https://merchant.example/failure')
          .setCheckoutURL('https://merchant.example/checkout'),
      'Checkout URL': () => RedirectionService.newBuilder()
          .setMerchantId('MID-123')
          .setSecuredKey('SECRET-abc')
          .setBasketID('BASKET-001')
          .setTxnAmount('1500.00')
          .setDescription('Order')
          .setCustomerEmail('c@example.com')
          .setCustomerMobile('03001234567')
          .setMerchantName('Store')
          .setSuccessURL('https://merchant.example/success')
          .setFailureURL('https://merchant.example/failure'),
    };

    cases.forEach((missingField, builder) {
      test('throws when $missingField is missing', () {
        expect(
          builder().build,
          throwsA(
            isA<Exception>().having(
              (e) => e.toString(),
              'message',
              contains(missingField),
            ),
          ),
        );
      });
    });
  });

  group('setAmount', () {
    test('keeps a whole-number amount intact', () {
      expect(validBuilder().setAmount('1500').build().amount, '1500');
    });

    test('rounds a decimal amount down', () {
      expect(validBuilder().setAmount('1500.40').build().amount, '1500');
    });

    test('rounds a decimal amount up', () {
      expect(validBuilder().setAmount('1500.60').build().amount, '1501');
    });

    test('tolerates surrounding whitespace', () {
      expect(validBuilder().setAmount(' 1500 ').build().amount, '1500');
    });

    test('throws ArgumentError on a non-numeric amount', () {
      expect(
        () => validBuilder().setAmount('1,500.00'),
        throwsA(isA<ArgumentError>()),
      );
    });

    test('throws ArgumentError on an empty amount', () {
      expect(() => validBuilder().setAmount(''), throwsA(isA<ArgumentError>()));
    });
  });

  group('setTxnAmount', () {
    test('sends the amount unchanged, decimals included', () {
      expect(validBuilder().setTxnAmount('1500.75').build().amount, '1500.75');
    });
  });

  test('the last amount setter wins', () {
    final service = validBuilder().setAmount('1500.60').build();
    expect(service.amount, '1501');

    final other = validBuilder().setAmount('1500.60').setTxnAmount('99.99');
    expect(other.build().amount, '99.99');
  });
}
